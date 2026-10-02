package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativePlaybackCancellationRejectsDelayedCalls(t *testing.T) {
	engine := &nativeEngine{}
	first, finishFirst, err := engine.nativeBeginPlayback(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	second, finishSecond, err := engine.nativeBeginPlayback(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer finishSecond()
	if !errors.Is(first.Err(), context.Canceled) {
		t.Fatal("old request was not canceled")
	}
	finishFirst()
	if second.Err() != nil {
		t.Fatal("old cleanup canceled the current request")
	}
	engine.nativeCancelPlayback(3)
	if !errors.Is(second.Err(), context.Canceled) {
		t.Fatal("active request was not canceled")
	}
	if _, _, err := engine.nativeBeginPlayback(context.Background(), 2); !errors.Is(err, context.Canceled) {
		t.Fatal("late request started after cancellation")
	}
	current, finishCurrent, err := engine.nativeBeginPlayback(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer finishCurrent()
	engine.nativeCancelPlayback(3)
	if current.Err() != nil {
		t.Fatal("stale cancellation stopped new playback")
	}
}

func TestNativeStreamLateResolutionPreservesCurrentSession(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := newNativeStreamServer(engine.downloader)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	media := providerMedia{URL: "https://example.test/master.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n"}
	current, currentToken := stream.nativeOpen(media)
	defer stream.nativeRelease(currentToken)
	_, lateToken := stream.nativeOpen(media)
	stream.nativeRelease(lateToken)
	response, err := http.Get(current)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("late resolution broke current playback: %d", response.StatusCode)
	}
}

func TestNativeHLSExtensionlessPlaylistsAndRedirectBase(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/root":
			http.Redirect(w, r, "/folder/master", http.StatusFound)
		case "/folder/master":
			w.Header().Set("Content-Type", "application/octet-stream")
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000\nvariant?signature=test\n")
		case "/folder/variant":
			if r.URL.Query().Get("signature") != "test" {
				t.Error("signature lost")
			}
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MAP:URI=\"init\"\n#EXTINF:2,\nsegment\n#EXT-X-ENDLIST\n")
		case "/folder/segment":
			io.WriteString(w, "synthetic media")
		default:
			t.Errorf("wrong redirect base: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
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
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/root"})
	defer stream.nativeRelease(token)
	read := func(address string) string {
		t.Helper()
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("read failed: %v, %d", err, response.StatusCode)
		}
		return string(body)
	}
	mediaLine := func(body string) string {
		t.Helper()
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "http://") {
				return line
			}
		}
		t.Fatal("missing media URL")
		return ""
	}
	child := mediaLine(read(address))
	if !strings.HasSuffix(child, ".m3u8") {
		t.Fatalf("playlist extension missing: %s", child)
	}
	playlist := read(child)
	if !strings.Contains(playlist, ".mp4\"") {
		t.Fatal("init extension missing")
	}
	segment := mediaLine(playlist)
	if !strings.HasSuffix(segment, ".ts") {
		t.Fatalf("segment extension missing: %s", segment)
	}
	if read(segment) != "synthetic media" {
		t.Fatal("segment body changed")
	}
}
