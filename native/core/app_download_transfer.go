package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type nativeDownloadAsset struct {
	address string
	name    string
	data    []byte
	key     bool
}

type nativeDownloadBundle struct {
	assets    []nativeDownloadAsset
	playlists map[string][]byte
	quality   int
}

type nativeDownloadFileState struct {
	URL       string `json:"url"`
	Validator string `json:"validator"`
	Total     int64  `json:"total"`
	Complete  bool   `json:"complete"`
}

func (manager *nativeDownloads) transferMedia(ctx context.Context, job nativeDownloadJob, media providerMedia) (nativeDownloadResult, error) {
	ctx = providerMediaContext(ctx, media.credentials)
	bundle := nativeDownloadBundle{quality: media.Quality, playlists: map[string][]byte{}}
	parsed, err := url.Parse(media.URL)
	if err != nil || !isProviderHTTPMediaURL(media.URL) {
		return nativeDownloadResult{}, errors.New("下载地址无效")
	}
	hls := media.Playlist != "" || strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8")
	entry := "media.mp4"
	if hls {
		bundle, err = manager.downloadBundle(ctx, media, job.Quality)
		if err != nil {
			return nativeDownloadResult{}, err
		}
		entry = "index.m3u8"
	} else {
		bundle.assets = []nativeDownloadAsset{{address: media.URL, name: entry}}
	}
	directory := manager.jobDirectory(job)
	identity := entry + "\x00" + media.URL + "\x00" + hex.EncodeToString(media.CENCKey) + "\x00" + strconv.Itoa(bundle.quality)
	if hls {
		for _, asset := range bundle.assets {
			identity += "\x00" + asset.address + "\x00" + string(asset.data)
		}
		encoded, _ := json.Marshal(bundle.playlists)
		identity += "\x00" + string(encoded)
	}
	fingerprint := sha256.Sum256([]byte(identity))
	marker := filepath.Join(directory, ".bundle")
	previous, _ := os.ReadFile(marker)
	if len(previous) > 0 && string(previous) != hex.EncodeToString(fingerprint[:]) {
		if err := os.RemoveAll(directory); err != nil {
			return nativeDownloadResult{}, errors.New("无法替换旧的下载文件")
		}
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nativeDownloadResult{}, errors.New("无法创建下载文件，请检查存储空间")
	}
	if err := nativeDownloadWrite(marker, []byte(hex.EncodeToString(fingerprint[:]))); err != nil {
		return nativeDownloadResult{}, err
	}
	var completedBytes int64
	for index, asset := range bundle.assets {
		if err := ctx.Err(); err != nil {
			return nativeDownloadResult{}, err
		}
		target := filepath.Join(directory, asset.name)
		var size int64
		if asset.data != nil {
			err = nativeDownloadWrite(target, asset.data)
			size = int64(len(asset.data))
		} else {
			size, err = manager.downloadFile(ctx, asset.address, media.Referer, target, asset.key, func(received, total int64) {
				progress := 0.0
				if total > 0 {
					progress = min(1, float64(received)/float64(total))
				}
				if hls {
					progress = (float64(index) + progress) / float64(len(bundle.assets))
				}
				overallTotal := int64(0)
				if !hls {
					overallTotal = total
				}
				manager.progress(job.ID, completedBytes+received, overallTotal, progress)
			})
		}
		if err != nil {
			return nativeDownloadResult{}, err
		}
		completedBytes += size
		manager.progress(job.ID, completedBytes, 0, float64(index+1)/float64(len(bundle.assets)))
	}
	if err := ctx.Err(); err != nil {
		return nativeDownloadResult{}, err
	}
	for name, body := range bundle.playlists {
		if err := nativeDownloadWrite(filepath.Join(directory, name), body); err != nil {
			return nativeDownloadResult{}, err
		}
	}
	manager.progress(job.ID, completedBytes, completedBytes, 1)
	return nativeDownloadResult{file: entry, key: hex.EncodeToString(media.CENCKey), quality: bundle.quality}, nil
}

func nativeDownloadValidator(header http.Header) string {
	if value := header.Get("ETag"); value != "" && !strings.HasPrefix(value, "W/") {
		return value
	}
	if value := header.Get("Last-Modified"); value != "" {
		if _, err := http.ParseTime(value); err == nil {
			return value
		}
	}
	return ""
}

func nativeDownloadRange(value string) (int64, int64, int64, bool) {
	var start, end, total int64
	if count, err := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total); err != nil || count != 3 {
		return 0, 0, 0, false
	}
	return start, end, total, start >= 0 && end >= start && total > end
}

func (manager *nativeDownloads) downloadFile(ctx context.Context, address, referer, target string, key bool, progress func(int64, int64)) (int64, error) {
	var metadata nativeDownloadFileState
	if data, err := os.ReadFile(target + ".json"); err == nil {
		_ = json.Unmarshal(data, &metadata)
	}
	if metadata.URL != address {
		metadata = nativeDownloadFileState{}
	}
	if metadata.Complete {
		if info, err := os.Stat(target); err == nil && info.Size() == metadata.Total && info.Size() > 0 && (!key || info.Size() == 16) {
			progress(info.Size(), info.Size())
			return info.Size(), nil
		}
	}
	partial := target + ".part"
	offset := int64(0)
	if metadata.Validator != "" && !key {
		if info, err := os.Stat(partial); err == nil && info.Size() > 0 && (metadata.Total <= 0 || info.Size() <= metadata.Total) {
			offset = info.Size()
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		size, restart, err := manager.downloadFileAttempt(ctx, address, referer, target, key, offset, metadata, progress)
		if !restart {
			return size, err
		}
		offset, metadata = 0, nativeDownloadFileState{}
	}
	return 0, errors.New("服务器无法恢复下载，请重试")
}

func (manager *nativeDownloads) downloadFileAttempt(ctx context.Context, address, referer, target string, key bool, offset int64, old nativeDownloadFileState, progress func(int64, int64)) (int64, bool, error) {
	if !isProviderHTTPMediaURL(address) {
		return 0, false, errors.New("媒体下载地址无效")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	inactivity := time.AfterFunc(30*time.Second, cancel)
	defer inactivity.Stop()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, address, nil)
	if err != nil {
		return 0, false, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		request.Header.Set("If-Range", old.Validator)
	}
	client := *manager.engine.downloader.client
	client.Timeout = 0
	response, err := manager.engine.downloader.doMediaRequestWithClient(request, &client)
	if err != nil {
		return 0, false, errors.New("下载连接中断，请检查网络后继续")
	}
	defer response.Body.Close()
	if offset > 0 && (response.StatusCode == http.StatusRequestedRangeNotSatisfiable || response.StatusCode == http.StatusPreconditionFailed) {
		return 0, true, nil
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return 0, false, nativeDownloadError(response.StatusCode)
	}
	if response.StatusCode == http.StatusOK {
		offset = 0
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return 0, false, errors.New("服务器返回了压缩媒体响应，无法安全续传")
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if !key && (strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/json")) {
		return 0, false, errors.New("站源返回了错误页面，请刷新下载地址后重试")
	}
	total := response.ContentLength
	validator := nativeDownloadValidator(response.Header)
	if response.StatusCode == http.StatusPartialContent {
		start, end, full, valid := nativeDownloadRange(response.Header.Get("Content-Range"))
		if !valid || start != offset || (response.ContentLength >= 0 && response.ContentLength != end-start+1) {
			return 0, false, errors.New("服务器返回的续传范围不正确，已停止下载")
		}
		if offset > 0 && ((validator != "" && validator != old.Validator) || (old.Total > 0 && old.Total != full)) {
			return 0, true, nil
		}
		total = full
	}
	if key && (total > 64 || offset > 0) {
		return 0, false, errors.New("视频密钥格式无效")
	}
	metadata := nativeDownloadFileState{URL: address, Validator: validator, Total: total}
	encoded, _ := json.Marshal(metadata)
	if err := nativeDownloadWrite(target+".json", encoded); err != nil {
		return 0, false, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	file, err := os.OpenFile(target+".part", flags, 0600)
	if err != nil {
		return 0, false, errors.New("无法写入下载文件，请检查存储空间")
	}
	received := offset
	buffer := make([]byte, 64<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if key && received+int64(n) > 64 {
				err = errors.New("视频密钥过大")
				break
			}
			written, writeErr := file.Write(buffer[:n])
			received += int64(written)
			if writeErr != nil || written != n {
				err = errors.New("写入下载文件失败，请检查剩余空间")
				break
			}
			inactivity.Reset(30 * time.Second)
			progress(received, total)
		}
		if readErr != nil {
			if readErr != io.EOF {
				err = errors.New("媒体文件未接收完整，点击继续下载")
			}
			break
		}
	}
	if err == nil {
		err = requestCtx.Err()
	}
	if err == nil && (received == 0 || (total >= 0 && received != total)) {
		err = errors.New("媒体文件未接收完整，点击继续下载")
	}
	if err == nil && key && received != 16 {
		err = errors.New("视频密钥长度无效")
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return received, false, err
	}
	if closeErr != nil {
		return received, false, closeErr
	}
	if err = os.Rename(target+".part", target); err != nil {
		return received, false, err
	}
	metadata.Complete, metadata.Total = true, received
	encoded, _ = json.Marshal(metadata)
	if err = nativeDownloadWrite(target+".json", encoded); err != nil {
		return received, false, err
	}
	return received, false, nil
}

func (manager *nativeDownloads) downloadPlaylist(ctx context.Context, address, referer string) (string, string, error) {
	return manager.engine.downloader.fetchMediaPlaylist(ctx, address, referer)
}
