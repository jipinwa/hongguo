package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nativePreloadKey struct{}

const nativePreloadLimit = 4 << 20

func (engine *nativeEngine) nativePreload(ctx context.Context, input nativeInput) (nativePlan, error) {
	ctx = context.WithValue(ctx, backgroundCatalogKey{}, true)
	ctx = context.WithValue(ctx, nativePreloadKey{}, true)
	plan, err := engine.nativeResolve(ctx, input)
	if err != nil || plan.Local {
		return plan, err
	}
	engine.mu.Lock()
	choice, exists := engine.playbacks[plan.Session]
	stream := engine.stream
	engine.mu.Unlock()
	if exists && stream != nil && choice.streamSession != "" {
		warming, cancel := context.WithTimeout(ctx, 6*time.Second)
		plan.PrefetchedBytes = stream.nativePrefetch(warming, choice.streamSession, plan.URL, input.Quality)
		cancel()
	}
	if err := ctx.Err(); err != nil {
		engine.nativeReleasePlayback(plan.Session)
		return nativePlan{}, err
	}
	return plan, nil
}

func (stream *nativeStreamServer) nativePrefetch(ctx context.Context, token, entry string, quality int) int64 {
	stream.mu.Lock()
	session := stream.sessions[token]
	stream.mu.Unlock()
	if session == nil {
		return 0
	}
	ctx, cancel := context.WithCancel(providerMediaContext(ctx, session.credentials))
	defer cancel()
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	remaining, requests := int64(nativePreloadLimit), 0
	visited := map[string]bool{}
	var warm func(string, int) bool
	warm = func(local string, depth int) bool {
		if ctx.Err() != nil || remaining <= 0 || requests >= 8 || depth > 2 || visited[local] {
			return false
		}
		visited[local] = true
		parsed, err := url.Parse(local)
		if err != nil || !strings.HasPrefix(local, stream.address+"/"+token+"/") {
			return false
		}
		id := strings.TrimPrefix(parsed.Path, "/"+token+"/")
		session.mu.Lock()
		asset, exists := session.assets[id]
		session.mu.Unlock()
		if !exists {
			return false
		}
		playlist := strings.Contains(asset.contentType, "mpegurl")
		if len(asset.data) == 0 {
			requests++
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.address, nil)
			if err != nil {
				return false
			}
			request.Header.Set("Referer", session.referer)
			if !playlist {
				request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", remaining-1))
			}
			response, err := stream.downloader.doMediaRequest(request)
			if err != nil {
				return false
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, remaining+1))
			response.Body.Close()
			if readErr != nil || len(body) == 0 || int64(len(body)) > remaining ||
				(response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent) {
				return false
			}
			if response.Request != nil && response.Request.URL != nil {
				asset.address = response.Request.URL.String()
			}
			asset.contentType = response.Header.Get("Content-Type")
			asset.etag, asset.modified = response.Header.Get("ETag"), response.Header.Get("Last-Modified")
			playlist = playlist || strings.Contains(strings.ToLower(asset.contentType), "mpegurl") || bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U"))
			asset.data = body
			asset.total = int64(len(body))
			if response.StatusCode == http.StatusPartialContent {
				start, end, total, valid := nativeContentRange(response.Header.Get("Content-Range"))
				if !valid || start != 0 || end+1 != int64(len(body)) {
					return false
				}
				asset.total = total
			}
			if asset.total > int64(len(body)) && asset.modified == "" && (asset.etag == "" || strings.HasPrefix(asset.etag, "W/")) {
				return false
			}
			if playlist {
				asset.contentType = "application/vnd.apple.mpegurl"
			}
			remaining -= int64(len(body))
			session.mu.Lock()
			session.assets[id] = asset
			session.mu.Unlock()
		}
		if !playlist {
			return true
		}
		lines, _, master := nativeHLSSelect(strings.Split(string(asset.data), "\n"), quality)
		rewritten, err := stream.nativeRewrite(token, session, strings.Join(lines, "\n"), asset.address)
		if err != nil {
			return false
		}
		segments := 0
		seconds := 0.0
		for _, raw := range strings.Split(rewritten, "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
				break
			}
			if strings.HasPrefix(line, "#EXTINF:") {
				value := strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0]
				duration, _ := strconv.ParseFloat(value, 64)
				seconds += duration
			}
			if strings.HasPrefix(line, "#EXT-X-KEY:") || strings.HasPrefix(line, "#EXT-X-MAP:") {
				for _, match := range nativePlaylistURI.FindAllStringSubmatch(line, -1) {
					if !strings.HasPrefix(match[1], "data:") && !warm(match[1], depth+1) {
						return false
					}
				}
			}
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !warm(line, depth+1) {
				break
			}
			segments++
			if master || segments >= 2 || seconds >= 8 {
				break
			}
		}
		return true
	}
	warm(entry, 0)
	return nativePreloadLimit - remaining
}

var nativeContentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func nativeContentRange(value string) (int64, int64, int64, bool) {
	match := nativeContentRangePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, 0, 0, false
	}
	start, first := strconv.ParseInt(match[1], 10, 64)
	end, second := strconv.ParseInt(match[2], 10, 64)
	total, third := strconv.ParseInt(match[3], 10, 64)
	return start, end, total, first == nil && second == nil && third == nil && start >= 0 && end >= start && total > end
}

func (stream *nativeStreamServer) nativeServePrefix(ctx context.Context, writer http.ResponseWriter, request *http.Request, session *nativeStreamSession, asset nativeStreamAsset) bool {
	start, end := int64(0), asset.total-1
	ranged := request.Header.Get("Range") != ""
	if ranged {
		value := request.Header.Get("Range")
		if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
			return false
		}
		parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
		if len(parts) != 2 {
			return false
		}
		var err error
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 || start >= int64(len(asset.data)) {
			return false
		}
		if parts[1] != "" {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return false
			}
			end = min(end, asset.total-1)
		}
	}
	validator := asset.etag
	if validator == "" || strings.HasPrefix(validator, "W/") {
		validator = asset.modified
	}
	if value := request.Header.Get("If-Range"); value != "" && value != validator {
		return false
	}
	var response *http.Response
	if end >= int64(len(asset.data)) && request.Method != http.MethodHead {
		upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.address, nil)
		if err != nil || validator == "" {
			return false
		}
		upstream.Header.Set("Referer", session.referer)
		upstream.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", len(asset.data), end))
		upstream.Header.Set("If-Range", validator)
		response, err = stream.nativeRequest(upstream)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		a, b, total, valid := nativeContentRange(response.Header.Get("Content-Range"))
		if response.StatusCode != http.StatusPartialContent || !valid || a != int64(len(asset.data)) || b != end || total != asset.total {
			session.invalidate(asset.address)
			return false
		}
		if validator == asset.etag && response.Header.Get("ETag") != validator ||
			validator == asset.modified && response.Header.Get("Last-Modified") != validator {
			session.invalidate(asset.address)
			return false
		}
	}
	writer.Header().Set("Content-Type", asset.contentType)
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if asset.etag != "" {
		writer.Header().Set("ETag", asset.etag)
	}
	if asset.modified != "" {
		writer.Header().Set("Last-Modified", asset.modified)
	}
	if ranged {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, asset.total))
		writer.WriteHeader(http.StatusPartialContent)
	}
	if request.Method == http.MethodHead {
		return true
	}
	_, err := writer.Write(asset.data[start:min(end+1, int64(len(asset.data)))])
	if err == nil && response != nil {
		_, _ = io.CopyN(writer, response.Body, end-int64(len(asset.data))+1)
	}
	return true
}

func (session *nativeStreamSession) invalidate(address string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	for id, asset := range session.assets {
		if asset.address == address {
			asset.data, asset.total, asset.etag, asset.modified = nil, 0, "", ""
			session.assets[id] = asset
		}
	}
}
