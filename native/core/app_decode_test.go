package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeHLSDecodesEncryptedSyntheticMedia(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is required for the optional synthetic decoder check")
	}
	directory := t.TempDir()
	key := []byte("0123456789abcdef")
	keyFile := filepath.Join(directory, "encryption.key")
	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	keyInfo := filepath.Join(directory, "key-info.txt")
	if err := os.WriteFile(keyInfo, []byte("denied.key\n"+keyFile+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	generate := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=160x90:r=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:v", "libx264", "-threads", "1",
		"-g", "10", "-pix_fmt", "yuv420p", "-c:a", "aac", "-hls_time", "1", "-hls_list_size", "0",
		"-hls_key_info_file", keyInfo, "-hls_segment_filename", filepath.Join(directory, "segment-%d.ts"), filepath.Join(directory, "index.m3u8"))
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate media: %v\n%s", err, output)
	}
	files := http.FileServer(http.Dir(directory))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/denied.key" {
			t.Error("decoder fetched the source key instead of the resolved key")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Referer") != "https://example.test/watch" {
			t.Error("decoder lost the referer")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		files.ServeHTTP(w, r)
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
	address, token := stream.nativeOpen(providerMedia{URL: upstream.URL + "/index.m3u8", HLSKey: key, Referer: "https://example.test/watch"})
	defer stream.nativeRelease(token)
	decode := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", address, "-f", "null", "-")
	if output, err := decode.CombinedOutput(); err != nil {
		t.Fatalf("decode rewritten encrypted HLS: %v\n%s", err, output)
	}
}
