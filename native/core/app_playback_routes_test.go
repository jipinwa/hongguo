package core

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestHongguoBackupAddressesAndCodecAlternatives(t *testing.T) {
	media, err := selectHongguoAppMedia(map[string]any{"video_list": []any{
		map[string]any{"main_url": "https://media.test/hevc.mp4", "video_meta": map[string]any{"codec_type": "hevc", "definition": "1080p"}},
		map[string]any{
			"main_url":    base64.StdEncoding.EncodeToString([]byte("https://media.test/main.mp4?token=a%2Fb")),
			"backup_url":  "https://backup.test/main.mp4?token=unchanged",
			"backup_urls": []any{"https://backup.test/main.mp4?token=unchanged", "file:///invalid", "https://third.test/main.mp4"},
			"video_meta":  map[string]any{"codec_type": "h264", "definition": "1080p"},
		},
		map[string]any{"main_url": "https://media.test/low.mp4", "video_meta": map[string]any{"codec_type": "h264", "definition": "720p"}},
		map[string]any{"main_url": "https://media.test/unsupported.mp4", "backup_url": "https://backup.test/unsupported.mp4", "video_meta": map[string]any{"codec_type": "bytevc2", "definition": "2160p"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	choices := nativePlaybackChoices(media, 0)
	var addresses []string
	for _, option := range choices.media {
		addresses = append(addresses, option.URL)
	}
	want := []string{
		"https://media.test/main.mp4?token=a%2Fb", "https://backup.test/main.mp4?token=unchanged",
		"https://third.test/main.mp4", "https://media.test/hevc.mp4", "https://media.test/low.mp4",
	}
	if !reflect.DeepEqual(addresses, want) || !reflect.DeepEqual(choices.qualities, []int{1080, 720}) {
		t.Fatalf("wrong fallback order: %+v; qualities=%v", addresses, choices.qualities)
	}
	if selected := nativePlaybackChoices(media, 1080); len(selected.media) != 4 {
		t.Fatal("manual quality must preserve its backup codecs and exclude lower quality")
	}
	if selected := nativePlaybackChoices(media, 720); len(selected.media) != 1 || selected.media[0].Quality != 720 {
		t.Fatal("manual 720p selection changed")
	}
	if selected := nativePlaybackChoices(media, 2160); len(selected.media) != len(want) {
		t.Fatal("an unavailable quality should use the episode's available routes")
	}
}

func TestNativePlaybackFallbackPreservesKeysAndReleasesOnlyOldSession(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := providerMedia{URL: "https://first.test/video.mp4", Referer: "https://first.test/", CENCKey: []byte("0123456789abcdef"), Quality: 1080}
	second := providerMedia{URL: "https://second.test/video.mp4", Referer: "https://second.test/watch", CENCKey: []byte("fedcba9876543210"), Quality: 1080}
	first.Variants = []providerMedia{first, first, second}
	plan, err := engine.nativeOpenPlayback(context.Background(), nativePlaybackChoices(first, 0))
	if err != nil || plan.RouteCount != 2 || plan.Session == "" {
		t.Fatalf("initial playback: %+v, %v", plan, err)
	}
	next, err := engine.nativeNextPlayback(context.Background(), plan.Session)
	if err != nil {
		t.Fatal(err)
	}
	if next.URL == "" || next.Headers["Referer"] != second.Referer || next.Key != "66656463626139383736353433323130" || next.RouteIndex != 1 || next.Session == plan.Session {
		t.Fatalf("alternate route lost its credentials: %+v", next)
	}
	engine.mu.Lock()
	nextChoice := engine.playbacks[next.Session]
	engine.mu.Unlock()
	engine.stream.mu.Lock()
	nextStream := engine.stream.sessions[nextChoice.streamSession]
	engine.stream.mu.Unlock()
	if nextStream == nil {
		t.Fatal("alternate route did not open a stream session")
	}
	foundSecond := false
	nextStream.mu.Lock()
	for _, asset := range nextStream.assets {
		if asset.address == second.URL {
			foundSecond = true
		}
	}
	nextStream.mu.Unlock()
	if !foundSecond {
		t.Fatalf("alternate route opened the wrong source: %+v", nextStream.assets)
	}
	engine.nativeReleasePlayback(plan.Session)
	if _, ok := engine.playbacks[next.Session]; !ok {
		t.Fatal("releasing the old plan removed the new playback")
	}
	if _, err := engine.nativeNextPlayback(context.Background(), next.Session); err == nil {
		t.Fatal("exhausted routes restarted at the beginning")
	}
	engine.nativeReleasePlayback(next.Session)
	if len(engine.playbacks) != 0 {
		t.Fatal("playback choices leaked")
	}
}

func TestNativePlaybackHLSAlternativesAreLazyAndIsolated(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	media := providerMedia{URL: "https://first.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-ENDLIST\n", Quality: 1080}
	media.Variants = []providerMedia{{URL: "https://backup.test/index.m3u8", Playlist: "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-ENDLIST\n", Quality: 720}}
	plan, err := engine.nativeOpenPlayback(context.Background(), nativePlaybackChoices(media, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.stream.server.Close()
	if len(engine.stream.sessions) != 1 {
		t.Fatal("unused alternatives opened HLS sessions")
	}
	current, err := engine.nativeNextPlayback(context.Background(), plan.Session)
	if err != nil {
		t.Fatal(err)
	}
	late, err := engine.nativeNextPlayback(context.Background(), plan.Session)
	if err != nil {
		t.Fatal(err)
	}
	engine.nativeReleasePlayback(late.Session)
	engine.nativeReleasePlayback(plan.Session)
	response, err := http.Get(current.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "#EXT-X-VERSION:3") {
		t.Fatalf("old/late cleanup broke the active HLS session: %s, %v", body, err)
	}
	engine.nativeReleasePlayback(current.Session)
	if len(engine.stream.sessions) != 0 || len(engine.playbacks) != 0 {
		t.Fatal("HLS fallback sessions leaked")
	}
}

func TestNativePlaybackCancellationAndCacheBound(t *testing.T) {
	engine := &nativeEngine{}
	choice := nativePlaybackChoices(providerMedia{URL: "https://example.test/video.mp4"}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.nativeOpenPlayback(ctx, choice); !errors.Is(err, context.Canceled) || len(engine.playbacks) != 0 {
		t.Fatal("canceled resolution created playback state")
	}
	for i := 0; i < 20; i++ {
		if _, err := engine.nativeOpenPlayback(context.Background(), choice); err != nil {
			t.Fatal(err)
		}
	}
	if len(engine.playbacks) != 8 {
		t.Fatal("abandoned playback choices are not bounded")
	}
	for token := range engine.playbacks {
		engine.nativeReleasePlayback(token)
	}
}
