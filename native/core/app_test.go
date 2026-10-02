package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeMetadataPreservesIdentityAndCover(t *testing.T) {
	value := true
	drama := nativeNormalize(Drama{ID: "hongguo:7399988776655443322", Title: "合成测试剧",
		Cover: map[string]any{"url": "https://example.test/poster.webp"}, TotalEpisode: json.Number("24"), VIP: &value})
	if drama.ID != "hongguo:7399988776655443322" || drama.SourceID != "7399988776655443322" ||
		drama.Cover != "https://example.test/poster.webp" || drama.Episodes != 24 || drama.VIP == nil || !*drama.VIP {
		t.Fatalf("metadata lost: %+v", drama)
	}
}

func TestNativeBoundaryRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"{", strings.Repeat("x", 1<<20+1)} {
		var result map[string]any
		if err := json.Unmarshal([]byte(NativeRequest(input)), &result); err != nil || result["ok"] != false {
			t.Fatal("invalid request accepted")
		}
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.nativeCatalog(context.Background(), nativeInput{Source: "invalid"}); err == nil {
		t.Fatal("invalid source accepted")
	}
	if _, err := engine.nativeDetail(context.Background(), nativeDrama{ID: "invalid"}); err == nil {
		t.Fatal("invalid drama accepted")
	}
}

func TestNativeHLSReadsNestedPlaylistKeyAndRanges(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://example.test/watch" {
			t.Error("media referer missing")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nvideo/media.m3u8\n")
		case "/video/media.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:3\n#EXT-X-KEY:METHOD=AES-128,URI=\"secret.key\",IV=0x00000000000000000000000000000001\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:3.0,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/video/secret.key":
			t.Error("the source key should be replaced by the resolved key")
			w.WriteHeader(http.StatusForbidden)
		case "/video/segment.ts":
			if r.Header.Get("Range") != "bytes=1-3" {
				t.Error("range missing")
			}
			w.Header().Set("Content-Range", "bytes 1-3/7")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, "123")
		default:
			io.WriteString(w, "init")
		}
	}))
	defer upstream.Close()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/master.m3u8",
		Referer: "https://example.test/watch", HLSKey: []byte("0123456789abcdef")})
	defer stream.nativeRelease(token)
	read := func(address string) string {
		t.Helper()
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("HTTP %d", response.StatusCode)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	master := read(address)
	child := ""
	for _, line := range strings.Split(master, "\n") {
		if strings.HasPrefix(line, "http:") {
			child = line
		}
	}
	if child == "" || strings.Contains(master, upstream.URL) {
		t.Fatal("master playlist not rewritten")
	}
	media := read(child)
	matches := nativePlaylistURI.FindAllStringSubmatch(media, -1)
	if len(matches) != 2 {
		t.Fatalf("missing rewritten key or init: %s", media)
	}
	if key := read(matches[0][1]); key != "0123456789abcdef" {
		t.Fatal("wrong key")
	}
	segment := ""
	for _, line := range strings.Split(media, "\n") {
		if strings.HasPrefix(line, "http:") {
			segment = line
		}
	}
	request, _ := http.NewRequest(http.MethodGet, segment, nil)
	request.Header.Set("Range", "bytes=1-3")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 206 || string(body) != "123" || response.Header.Get("Content-Range") != "bytes 1-3/7" {
		t.Fatal("range was not preserved")
	}
	stream.nativeRelease(token)
	response, err = http.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatal("released stream remained accessible")
	}
}

func TestHongguoDetailAndPlayableQuality(t *testing.T) {
	detail := map[string]any{"series_id": "700001", "series_title": "合成测试", "episode_cnt": 2,
		"video_list": []any{
			map[string]any{"vid": "800002", "vid_index": 2, "series_id": "700001"},
			map[string]any{"vid": "800001", "vid_index": 1, "series_id": "700001"},
		}}
	result := map[string]any{"data": map[string]any{"video_data": detail}}
	entry, err := parseHongguoAppDetail(result, "700001")
	if err != nil || len(entry.Chapters) != 2 || entry.Chapters[0].VideoURL != "hongguo-cenc://800001" {
		t.Fatalf("wrong episode ordering: %v", err)
	}
	if _, err := parseHongguoAppDetail(result, "700002"); err == nil {
		t.Fatal("wrong drama accepted")
	}
	detail["episode_cnt"] = 3
	if _, err := parseHongguoAppDetail(result, "700001"); err == nil {
		t.Fatal("incomplete details accepted")
	}
	media, err := selectHongguoAppMedia(map[string]any{"video_list": []any{
		map[string]any{"main_url": "https://media.example.test/new.mp4", "video_meta": map[string]any{"codec_type": "bytevc2", "definition": "2160p"}},
		map[string]any{"main_url": "https://media.example.test/low.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "720p"}},
		map[string]any{"main_url": "https://media.example.test/high.mp4", "video_meta": map[string]any{"codec_type": "hevc", "definition": "1080p"}},
	}})
	if err != nil || media.Quality != 1080 || !strings.HasSuffix(media.URL, "high.mp4") {
		t.Fatalf("wrong playable quality: %v", err)
	}
}
