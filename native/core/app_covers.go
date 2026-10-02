package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const nativeCoverMaxBytes = 20 << 20
const nativeCoverCacheBytes = 256 << 20
const nativeCoverCacheEntries = 2000
const nativeCoverTTL = 30 * 24 * time.Hour

type nativeCoverEntry struct {
	size      int64
	updatedAt time.Time
	usedAt    time.Time
}

type nativeCoverCall struct {
	done chan struct{}
	path string
	err  error
}

type nativeCoverCache struct {
	directory   string
	downloader  *Downloader
	mu          sync.Mutex
	entries     map[string]nativeCoverEntry
	pending     map[string]*nativeCoverCall
	slots       chan struct{}
	repairSlots chan struct{}
	repairs     map[string]*nativeCoverRepair
	size        int64
	limit       int64
}

func newNativeCoverCache(directory string, downloader *Downloader) *nativeCoverCache {
	cache := &nativeCoverCache{
		directory: filepath.Join(directory, "covers-v1"), downloader: downloader,
		entries: map[string]nativeCoverEntry{}, pending: map[string]*nativeCoverCall{},
		slots: make(chan struct{}, 4), limit: nativeCoverCacheBytes,
		repairSlots: make(chan struct{}, 1), repairs: map[string]*nativeCoverRepair{},
	}
	files, _ := os.ReadDir(cache.directory)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".decode-") && !file.IsDir() {
			if info, err := file.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(cache.directory, file.Name()))
			}
		}
		key, valid := strings.CutSuffix(file.Name(), ".img")
		if !valid || len(key) != 64 || file.IsDir() {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > nativeCoverMaxBytes {
			continue
		}
		cache.entries[key] = nativeCoverEntry{size: info.Size(), updatedAt: info.ModTime(), usedAt: info.ModTime()}
		cache.size += info.Size()
	}
	cache.prune("")
	return cache
}

func nativeCoverReferer(downloader *Downloader, source, address string) string {
	if source == sourceCloudFront {
		if parsed, err := url.Parse(address); err == nil && (strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".zdmhyg.cn") || strings.EqualFold(parsed.Hostname(), "pic.tuafjz.cn")) {
			return downloader.providerBaseURL(sourceHuangguoAI) + "/"
		}
	}
	if source == sourceHuangdou {
		if parsed, err := url.Parse(address); err == nil && (strings.EqualFold(parsed.Hostname(), "tideember.cc") || strings.EqualFold(parsed.Hostname(), "xqjurgek.top")) {
			return parsed.Scheme + "://" + parsed.Host + "/home"
		}
		downloader.providerMu.Lock()
		host := downloader.providerHosts[source]
		downloader.providerMu.Unlock()
		if host == "" {
			host = downloader.providerBaseURL(source)
		}
		return host + "/home"
	}
	return downloader.providerBaseURL(source) + "/"
}

func validNativeCoverURL(address *url.URL) bool {
	return address != nil && (address.Scheme == "https" || address.Scheme == "http") && address.Hostname() != "" && address.User == nil
}

func (cache *nativeCoverCache) loadAddress(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	drama.Cover = repairLegacyCoverURL(drama)
	address, err := url.Parse(drama.Cover)
	if err != nil || !validNativeCoverURL(address) {
		return "", errors.New("海报地址无效")
	}
	source := canonicalProviderSource(drama.Source)
	if source == "" {
		source = sourceFromDramaID(drama.ID)
	}
	referer := nativeCoverReferer(cache.downloader, source, drama.Cover)
	digest := sha256.Sum256([]byte(source + "\x00" + drama.Cover + "\x00" + referer))
	key := hex.EncodeToString(digest[:])
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	fallback := ""
	if entry, found := cache.entries[key]; found {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == entry.size {
			entry.usedAt = time.Now()
			cache.entries[key] = entry
			age := time.Since(entry.updatedAt)
			if !force && age >= 0 && age < nativeCoverTTL {
				cache.mu.Unlock()
				return path, nil
			}
			if !force {
				fallback = path
			}
		} else {
			delete(cache.entries, key)
			cache.size -= entry.size
		}
	}
	call := cache.pending[key]
	if call == nil {
		if len(cache.pending) >= 256 {
			cache.mu.Unlock()
			return "", errors.New("海报正在加载，请稍后重试")
		}
		call = &nativeCoverCall{done: make(chan struct{})}
		cache.pending[key] = call
		go cache.fetch(key, drama, referer, fallback, call)
	}
	cache.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-call.done:
		return call.path, call.err
	}
}

func (cache *nativeCoverCache) fetch(key string, drama nativeDrama, referer, fallback string, call *nativeCoverCall) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var data []byte
	var err error
	select {
	case cache.slots <- struct{}{}:
		data, err = cache.download(ctx, drama.Cover, referer)
		<-cache.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	path := filepath.Join(cache.directory, key+".img")
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err == nil {
		err = writeNativeCacheFile(path, data)
		if err == nil {
			cache.size -= cache.entries[key].size
			now := time.Now()
			cache.entries[key] = nativeCoverEntry{size: int64(len(data)), updatedAt: now, usedAt: now}
			cache.size += int64(len(data))
			cache.prune(key)
			call.path = path
		}
	}
	if err != nil && fallback != "" {
		if _, statErr := os.Stat(fallback); statErr == nil {
			call.path, err = fallback, nil
		}
	}
	call.err = err
	delete(cache.pending, key)
	close(call.done)
	if err != nil {
		cache.downloader.recordDiagnostic(diagnosticEvent{Event: "cover_failed", Source: drama.Source, DramaID: drama.ID, Message: err.Error()})
	}
}

func (cache *nativeCoverCache) download(ctx context.Context, address, referer string) ([]byte, error) {
	request, err := http.NewRequestWithContext(context.WithValue(ctx, nativeCoverNetworkKey{}, true), http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "image/webp,image/jpeg,image/png,image/gif,*/*;q=0.5")
	request.Header.Set("Sec-Fetch-Mode", "no-cors")
	request.Header.Set("Sec-Fetch-Dest", "image")
	client := *cache.downloader.client
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validNativeCoverURL(request.URL) {
			return errors.New("海报重定向地址无效")
		}
		request.Header.Set("Referer", referer)
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("海报请求失败：HTTP %d", response.StatusCode)
	}
	if response.ContentLength > nativeCoverMaxBytes {
		return nil, errors.New("海报文件过大")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeCoverMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > nativeCoverMaxBytes {
		return nil, errors.New("海报文件过大")
	}
	data = nativeDecodeCover(data)
	if !nativeIsCoverImage(data) {
		return nil, errors.New("站源未返回有效海报")
	}
	return data, nil
}

func nativeDecodeCover(data []byte) []byte {
	if nativeIsCoverImage(data) {
		return data
	}
	encrypted := data
	if bytes.HasPrefix(encrypted, []byte("Salted__")) && len(encrypted) > 16 {
		encrypted = encrypted[16:]
	}
	if len(encrypted) > 0 {
		block, err := aes.NewCipher([]byte("f5d965df75336270"))
		if err == nil {
			plain := make([]byte, (len(encrypted)+aes.BlockSize-1)/aes.BlockSize*aes.BlockSize)
			copy(plain, encrypted)
			cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(plain, plain)
			plain = plain[:len(encrypted)]
			if unpadded, err := pkcs7Unpad(plain, aes.BlockSize); err == nil {
				plain = unpadded
			}
			if nativeIsCoverImage(plain) {
				return plain
			}
		}
	}
	plain := append([]byte(nil), data...)
	key := []byte("2019ysapp7527")
	for index := 0; index < min(100, len(plain)); index++ {
		plain[index] ^= key[index%len(key)]
	}
	if nativeIsCoverImage(plain) {
		return plain
	}
	return data
}

func nativeIsCoverImage(data []byte) bool {
	if isHEICImage(data) {
		return true
	}
	if bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) ||
		bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) || bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if len(data) >= 16 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis", "heic", "heix", "hevc", "hevx", "mif1":
			return true
		}
	}
	return false
}

func (cache *nativeCoverCache) prune(keep string) {
	if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
		return
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		if key != keep {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return cache.entries[keys[i]].usedAt.Before(cache.entries[keys[j]].usedAt) })
	for _, key := range keys {
		if cache.size <= cache.limit && len(cache.entries) <= nativeCoverCacheEntries {
			break
		}
		if err := os.Remove(filepath.Join(cache.directory, key+".img")); err == nil || errors.Is(err, os.ErrNotExist) {
			cache.size -= cache.entries[key].size
			delete(cache.entries, key)
		}
	}
}
