package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var nativeDownloadAttributes = regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)

func nativeHLSAttributes(line string) map[string]string {
	result := map[string]string{}
	for _, match := range nativeDownloadAttributes.FindAllStringSubmatch(line, -1) {
		result[match[1]] = strings.Trim(match[2], "\"")
	}
	return result
}

func nativeHLSSelect(lines []string, quality int) ([]string, int, bool) {
	type variant struct {
		line, uri, height, bandwidth int
		attributes                   map[string]string
	}
	var variants []variant
	excluded := map[int]bool{}
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF:") {
			continue
		}
		attributes := nativeHLSAttributes(line)
		height := 0
		if resolution := strings.Split(attributes["RESOLUTION"], "x"); len(resolution) == 2 {
			height, _ = strconv.Atoi(resolution[1])
		}
		bandwidth, _ := strconv.Atoi(attributes["BANDWIDTH"])
		for next := index + 1; next < len(lines); next++ {
			text := strings.TrimSpace(lines[next])
			if text == "" || strings.HasPrefix(text, "#") {
				continue
			}
			variants = append(variants, variant{index, next, height, bandwidth, attributes})
			excluded[index], excluded[next] = true, true
			break
		}
	}
	if len(variants) == 0 {
		return lines, 0, false
	}
	best := variants[0]
	for _, candidate := range variants[1:] {
		exact, bestExact := quality > 0 && candidate.height == quality, quality > 0 && best.height == quality
		if (exact && !bestExact) || (exact == bestExact && (candidate.height > best.height ||
			(candidate.height == best.height && candidate.bandwidth > best.bandwidth))) {
			best = candidate
		}
	}
	delete(excluded, best.line)
	delete(excluded, best.uri)
	groups := map[string]int{}
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-MEDIA:") {
			continue
		}
		attributes := nativeHLSAttributes(line)
		kind := attributes["TYPE"]
		if attributes["GROUP-ID"] != best.attributes[kind] {
			continue
		}
		if _, exists := groups[kind]; !exists || attributes["DEFAULT"] == "YES" {
			groups[kind] = index
		}
	}
	var output []string
	for index, line := range lines {
		text := strings.TrimSpace(line)
		if excluded[index] || strings.HasPrefix(text, "#EXT-X-I-FRAME-STREAM-INF:") ||
			strings.HasPrefix(text, "#EXT-X-SESSION-DATA:") {
			continue
		}
		if strings.HasPrefix(text, "#EXT-X-MEDIA:") {
			if selected, exists := groups[nativeHLSAttributes(line)["TYPE"]]; !exists || selected != index {
				continue
			}
		}
		output = append(output, line)
	}
	return output, best.height, true
}

func nativeDownloadAssetName(address, extension string) string {
	digest := sha256.Sum256([]byte(address))
	return hex.EncodeToString(digest[:16]) + extension
}

func (manager *nativeDownloads) downloadBundle(ctx context.Context, media providerMedia, quality int) (nativeDownloadBundle, error) {
	ctx = providerMediaContext(ctx, media.credentials)
	bundle := nativeDownloadBundle{quality: media.Quality, playlists: map[string][]byte{}}
	visited := map[string]string{}
	pending := map[string]bool{}
	assets := map[string]bool{}
	var walk func(string, string, string, int) (string, error)
	walk = func(address, body, name string, depth int) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if pending[address] {
			return "", errors.New("播放列表存在循环引用")
		}
		if previous := visited[address]; previous != "" {
			return previous, nil
		}
		if depth > 6 || len(visited) > 100 {
			return "", errors.New("播放列表嵌套过多")
		}
		pending[address] = true
		defer delete(pending, address)
		base := address
		if body == "" {
			var err error
			body, base, err = manager.downloadPlaylist(ctx, address, media.Referer)
			if err != nil {
				return "", err
			}
		}
		body = strings.TrimSpace(strings.TrimPrefix(body, "\ufeff"))
		if !strings.HasPrefix(body, "#EXTM3U") || len(body) > 4<<20 {
			return "", errors.New("下载播放列表无效")
		}
		lines, height, master := nativeHLSSelect(strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n"), quality)
		if depth == 0 && height > 0 {
			bundle.quality = height
		}
		if !master && !strings.Contains(body, "#EXT-X-ENDLIST") {
			return "", errors.New("暂不支持下载直播或尚未结束的播放列表")
		}
		parsed, err := url.Parse(base)
		if err != nil {
			return "", errors.New("播放列表地址无效")
		}
		rewrite := func(reference string, playlist, key bool) (string, error) {
			relative, err := url.Parse(reference)
			if err != nil {
				return "", errors.New("分片地址无效")
			}
			resolved := parsed.ResolveReference(relative).String()
			if playlist {
				return walk(resolved, "", nativeDownloadAssetName(resolved, ".m3u8"), depth+1)
			}
			var data []byte
			if key && len(media.HLSKey) == 16 {
				data = append([]byte{}, media.HLSKey...)
			} else if strings.HasPrefix(resolved, "data:") && key {
				fields := strings.SplitN(resolved, ",", 2)
				if len(fields) != 2 {
					return "", errors.New("内嵌视频密钥无效")
				}
				if strings.HasSuffix(fields[0], ";base64") {
					data, err = base64.StdEncoding.DecodeString(fields[1])
				} else {
					var text string
					text, err = url.PathUnescape(fields[1])
					data = []byte(text)
				}
				if err != nil || len(data) != 16 {
					return "", errors.New("内嵌视频密钥无效")
				}
			} else if !isProviderHTTPMediaURL(resolved) {
				return "", errors.New("分片地址不支持下载")
			}
			extension := strings.ToLower(path.Ext(relative.Path))
			switch extension {
			case ".ts", ".mp4", ".m4s", ".aac", ".m4a", ".vtt", ".key":
			default:
				extension = ".bin"
			}
			if key {
				extension = ".key"
			}
			file := nativeDownloadAssetName(resolved, extension)
			if !assets[file] {
				if len(assets) >= 20000 {
					return "", errors.New("视频分片数量过多")
				}
				assets[file] = true
				bundle.assets = append(bundle.assets, nativeDownloadAsset{address: resolved, name: file, data: data, key: key})
			}
			return file, nil
		}
		var output []string
		nextPlaylist := false
		for _, line := range lines {
			text := strings.TrimSpace(line)
			if strings.HasPrefix(text, "#EXT-X-DEFINE:") || strings.Contains(text, "{$") {
				return "", errors.New("暂不支持下载使用变量地址的播放列表")
			}
			key := strings.HasPrefix(text, "#EXT-X-KEY:") || strings.HasPrefix(text, "#EXT-X-SESSION-KEY:")
			if key {
				attributes := nativeHLSAttributes(text)
				if method := attributes["METHOD"]; method != "AES-128" && method != "NONE" {
					return "", errors.New("暂不支持离线保存该视频的加密方式")
				}
				if format := attributes["KEYFORMAT"]; format != "" && format != "identity" {
					return "", errors.New("该视频需要源站授权，暂不支持离线保存")
				}
			}
			if text != "" && !strings.HasPrefix(text, "#") {
				line, err = rewrite(text, nextPlaylist, false)
				nextPlaylist = false
				if err != nil {
					return "", err
				}
			} else if strings.Contains(text, "URI=") {
				for _, match := range nativePlaylistURI.FindAllStringSubmatch(line, -1) {
					playlist := strings.HasPrefix(text, "#EXT-X-MEDIA:")
					local, err := rewrite(match[1], playlist, key)
					if err != nil {
						return "", err
					}
					line = strings.ReplaceAll(line, match[0], "URI=\""+local+"\"")
				}
			}
			if strings.HasPrefix(text, "#EXT-X-STREAM-INF:") {
				nextPlaylist = true
			}
			output = append(output, line)
		}
		bundle.playlists[name] = []byte(strings.Join(output, "\n") + "\n")
		visited[address] = name
		return name, nil
	}
	_, err := walk(media.URL, media.Playlist, "index.m3u8", 0)
	if err != nil {
		return bundle, err
	}
	if len(bundle.assets) == 0 {
		return bundle, errors.New("播放列表没有可下载的媒体")
	}
	return bundle, nil
}
