package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func downloadTestManager(t *testing.T) *nativeDownloads {
	t.Helper()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	return engine.downloads
}

func downloadTestInput(count int) nativeInput {
	input := nativeInput{Drama: nativeDrama{ID: "hongguo:100", Source: "hongguo", Title: "合成下载测试"}, Quality: 1080}
	for index := 1; index <= count; index++ {
		input.Entries = append(input.Entries, nativeDownloadEpisode{Index: index,
			Chapter: Chapter{ID: strconv.Itoa(index), CurrentEpisode: json.RawMessage(strconv.Itoa(index))}})
	}
	return input
}

func waitDownloads(t *testing.T, manager *nativeDownloads, predicate func([]nativeDownloadJob) bool) []nativeDownloadJob {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := manager.snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if predicate(jobs) {
			return jobs
		}
		time.Sleep(10 * time.Millisecond)
	}
	jobs, _ := manager.snapshot()
	t.Fatalf("download queue did not settle: %+v", jobs)
	return nil
}

func TestNativeDownloadsOriginalBytesPersistenceAndLocalPriority(t *testing.T) {
	data := bytes.Repeat([]byte("synthetic-original-media"), 8192)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://synthetic.test/watch" {
			t.Error("download lost referer")
		}
		w.Header().Set("ETag", "\"original\"")
		http.ServeContent(w, r, "media.mp4", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(upstream.Close)
	manager := downloadTestManager(t)
	key := []byte("0123456789abcdef")
	manager.resolve = func(context.Context, nativeDownloadJob) (providerMedia, error) {
		return providerMedia{URL: upstream.URL + "/media.mp4", Referer: "https://synthetic.test/watch",
			CENCKey: key, Quality: 1080}, nil
	}
	input := downloadTestInput(2)
	if n, err := manager.enqueue(input); err != nil || n != 2 {
		t.Fatalf("enqueue: %d, %v", n, err)
	}
	if n, err := manager.enqueue(input); err != nil || n != 0 {
		t.Fatalf("duplicate enqueue: %d, %v", n, err)
	}
	jobs := waitDownloads(t, manager, func(jobs []nativeDownloadJob) bool {
		return len(jobs) == 2 && jobs[0].State == "completed" && jobs[1].State == "completed"
	})
	manager.close()
	reopened := newNativeDownloads(manager.engine)
	t.Cleanup(reopened.close)
	manager.engine.downloads = reopened
	upstream.Close()
	for _, job := range jobs {
		plan, err := manager.engine.nativeResolve(context.Background(), nativeInput{Drama: job.Drama, Index: job.Index})
		if err != nil || !plan.Local || plan.Key != hex.EncodeToString(key) || plan.Quality != 1080 || plan.Session != "" {
			t.Fatalf("download must play locally after restart: %+v, %v", plan, err)
		}
		got, err := os.ReadFile(plan.URL)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("source bytes changed: %v", err)
		}
		if job.Bytes != int64(len(data)) || job.Progress != 1 {
			t.Fatalf("wrong completed progress: %+v", job)
		}
	}
	id := nativeDownloadID(input.Drama.ID, 1)
	if err := os.Remove(filepath.Join(reopened.root, id, "media.mp4")); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := manager.engine.nativeResolve(context.Background(), nativeInput{Drama: input.Drama, Index: 1})
		if !errors.Is(err, errNativeLocalFile) {
			t.Fatalf("missing local file silently used network: %v", err)
		}
	}
	if err := reopened.control(id, "remove"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := reopened.localPlan(input.Drama.ID, 1); found || err != nil {
		t.Fatal("removed job still selected")
	}
}

func TestNativeDownloadRangeResumeAndAddressIsolation(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 65536)
	var mu sync.Mutex
	var ranges, validators []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		validators = append(validators, r.Header.Get("If-Range"))
		mu.Unlock()
		w.Header().Set("ETag", "\"v1\"")
		http.ServeContent(w, r, "media.mp4", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(upstream.Close)
	manager := downloadTestManager(t)
	target := filepath.Join(manager.root, "range.mp4")
	address := upstream.URL + "/media.mp4"
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := manager.downloadFile(ctx, address, "", target, false, func(n, total int64) {
		if n >= 4096 {
			cancel()
		}
	}); err == nil {
		t.Fatal("interrupted download marked complete")
	}
	part, err := os.Stat(target + ".part")
	if err != nil || part.Size() <= 0 || part.Size() >= int64(len(data)) {
		t.Fatalf("no resumable partial: %v", err)
	}
	if _, err := manager.downloadFile(context.Background(), address, "", target, false, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(ranges) != 2 || ranges[1] != fmt.Sprintf("bytes=%d-", part.Size()) || validators[1] != "\"v1\"" {
		t.Fatalf("resume headers: %v %v", ranges, validators)
	}
	mu.Unlock()
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, data) {
		t.Fatal("range resume corrupted media")
	}
	if _, err := manager.downloadFile(context.Background(), upstream.URL+"/other.mp4", "", target, false, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranges) != 3 || ranges[2] != "" {
		t.Fatal("a different address reused old bytes")
	}
}

func TestNativeDownloadRangeServersCannotMixFiles(t *testing.T) {
	for _, mode := range []string{"ignored", "changed", "invalid", "no-validator", "range-rejected"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("a-completely-new-synthetic-file")
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("ETag", "\"new\"")
				if r.Header.Get("Range") != "" {
					switch mode {
					case "changed", "invalid":
						start := 4
						if mode == "invalid" {
							start = 5
						}
						w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
						w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
						w.WriteHeader(http.StatusPartialContent)
						_, _ = w.Write(data[start:])
						return
					case "range-rejected":
						w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
						return
					case "no-validator":
						t.Error("resumed without a validator")
					}
				}
				_, _ = w.Write(data)
			}))
			t.Cleanup(upstream.Close)
			manager := downloadTestManager(t)
			target := filepath.Join(manager.root, "resume.mp4")
			metadata := nativeDownloadFileState{URL: upstream.URL + "/media", Validator: "\"old\"", Total: int64(len(data))}
			if mode == "no-validator" {
				metadata.Validator = ""
			}
			encoded, _ := json.Marshal(metadata)
			if err := os.WriteFile(target+".json", encoded, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target+".part", []byte("OLD!"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := manager.downloadFile(context.Background(), metadata.URL, "", target, false, func(int64, int64) {})
			if mode == "invalid" {
				if err == nil {
					t.Fatal("accepted an invalid range")
				}
				partial, _ := os.ReadFile(target + ".part")
				if string(partial) != "OLD!" {
					t.Fatal("invalid range modified the partial")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(target)
			if !bytes.Equal(got, data) {
				t.Fatalf("mixed old and new bytes: %q", got)
			}
			want := int32(1)
			if mode == "changed" || mode == "range-rejected" {
				want = 2
			}
			if requests.Load() != want {
				t.Fatalf("unexpected request count: %d", requests.Load())
			}
		})
	}
}

func TestNativeDownloadIncompleteMediaAndInvalidKeyStayIncomplete(t *testing.T) {
	for _, mode := range []string{"truncated", "html", "json", "key", "encoded"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "truncated":
					w.Header().Set("Content-Length", "999")
				case "html":
					w.Header().Set("Content-Type", "text/html")
				case "json":
					w.Header().Set("Content-Type", "application/json")
				case "encoded":
					w.Header().Set("Content-Encoding", "gzip")
				}
				_, _ = w.Write([]byte("invalid-short-content"))
			}))
			t.Cleanup(upstream.Close)
			manager := downloadTestManager(t)
			target := filepath.Join(manager.root, "incomplete.mp4")
			_, err := manager.downloadFile(context.Background(), upstream.URL, "", target, mode == "key", func(int64, int64) {})
			if err == nil {
				t.Fatal("invalid media marked completed")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("incomplete media published")
			}
		})
	}
}

func TestNativeDownloadsBoundedQueueCancelAndPlaybackIsolation(t *testing.T) {
	data := bytes.Repeat([]byte("original"), 256)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(upstream.Close)
	manager := downloadTestManager(t)
	var active, peak atomic.Int32
	started := make(chan int, 20)
	release := make(chan struct{})
	manager.resolve = func(ctx context.Context, job nativeDownloadJob) (providerMedia, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- job.Index
		select {
		case <-ctx.Done():
			return providerMedia{}, ctx.Err()
		case <-release:
			return providerMedia{URL: upstream.URL + "/media.mp4", Quality: 1080}, nil
		}
	}
	input := downloadTestInput(4)
	if _, err := manager.enqueue(input); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("queue did not start")
		}
	}
	ctx, finish, err := manager.engine.nativeBeginPlayback(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	manager.engine.nativeCancelPlayback(2)
	finish()
	if ctx.Err() == nil || active.Load() != 2 {
		t.Fatal("playback cancellation affected downloads")
	}
	first := nativeDownloadID(input.Drama.ID, 1)
	if err := manager.control(first, "pause"); err != nil {
		t.Fatal(err)
	}
	if err := manager.control(first, "resume"); err != nil {
		t.Fatal(err)
	}
	waitDownloads(t, manager, func(jobs []nativeDownloadJob) bool {
		for _, job := range jobs {
			if job.ID == first {
				return job.State == "downloading"
			}
		}
		return false
	})
	if err := manager.control(first, "remove"); err != nil {
		t.Fatal(err)
	}
	waitDownloads(t, manager, func(jobs []nativeDownloadJob) bool { return len(jobs) == 3 })
	if err := manager.control("", "pauseAll"); err != nil {
		t.Fatal(err)
	}
	waitDownloads(t, manager, func(jobs []nativeDownloadJob) bool {
		for _, job := range jobs {
			if job.State != "paused" {
				return false
			}
		}
		return true
	})
	close(release)
	if err := manager.control("", "resumeAll"); err != nil {
		t.Fatal(err)
	}
	waitDownloads(t, manager, func(jobs []nativeDownloadJob) bool {
		for _, job := range jobs {
			if job.State != "completed" {
				return false
			}
		}
		return true
	})
	manager.close()
	if peak.Load() > 2 {
		t.Fatalf("excess concurrency: %d", peak.Load())
	}
	if _, err := os.Stat(filepath.Join(manager.root, first)); !os.IsNotExist(err) {
		t.Fatal("removed job recreated files")
	}
}

func TestNativeDownloadsRestartPausesPendingJobs(t *testing.T) {
	manager := downloadTestManager(t)
	manager.resolve = func(ctx context.Context, job nativeDownloadJob) (providerMedia, error) {
		<-ctx.Done()
		return providerMedia{}, ctx.Err()
	}
	input := downloadTestInput(3)
	if _, err := manager.enqueue(input); err != nil {
		t.Fatal(err)
	}
	manager.close()
	reopened := newNativeDownloads(manager.engine)
	t.Cleanup(reopened.close)
	jobs, err := reopened.snapshot()
	if err != nil || len(jobs) != 3 {
		t.Fatalf("lost persisted queue: %+v %v", jobs, err)
	}
	for _, job := range jobs {
		if job.State != "paused" {
			t.Fatalf("restart automatically downloaded: %+v", job)
		}
	}
	if len(reopened.active) != 0 {
		t.Fatal("restart started workers")
	}
}

func TestNativeDownloadHLSBundleIncludesSelectedAudioKeysAndByteRanges(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"Main\",DEFAULT=YES,URI=\"audio.m3u8\"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"Other\",URI=\"other.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=2000,RESOLUTION=1920x1080,AUDIO=\"audio\"\nhigh.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=1000,RESOLUTION=1280x720,AUDIO=\"audio\"\nlow.m3u8\n")
		case "/low.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\",IV=0x00000000000000000000000000000001\n#EXTINF:2,\n#EXT-X-BYTERANGE:4@0\nmedia.mp4\n#EXT-X-DISCONTINUITY\n#EXTINF:2,\n#EXT-X-BYTERANGE:4@4\nmedia.mp4\n#EXT-X-ENDLIST\n")
		case "/audio.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\naudio.aac\n#EXT-X-ENDLIST\n")
		case "/key":
			_, _ = w.Write([]byte("0123456789abcdef"))
		case "/media.mp4", "/init.mp4", "/audio.aac":
			_, _ = w.Write([]byte("12345678"))
		default:
			t.Errorf("unselected HLS resource fetched: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	manager := downloadTestManager(t)
	job := downloadTestInput(1)
	record := nativeDownloadJob{ID: nativeDownloadID(job.Drama.ID, 1), Drama: job.Drama, Index: 1, Quality: 720}
	result, err := manager.transferMedia(context.Background(), record, providerMedia{URL: upstream.URL + "/master.m3u8"})
	if err != nil || result.file != "index.m3u8" || result.quality != 720 {
		t.Fatalf("bundle failed: %+v %v", result, err)
	}
	directory := filepath.Join(manager.root, record.ID)
	paths, _ := filepath.Glob(filepath.Join(directory, "*.m3u8"))
	if len(paths) != 3 {
		t.Fatalf("wrong playlist selection: %v", paths)
	}
	combined := ""
	for _, path := range paths {
		body, _ := os.ReadFile(path)
		combined += string(body)
		for _, line := range strings.Split(string(body), "\n") {
			var references []string
			if line != "" && !strings.HasPrefix(line, "#") {
				references = append(references, line)
			}
			for _, match := range nativePlaylistURI.FindAllStringSubmatch(line, -1) {
				references = append(references, match[1])
			}
			for _, reference := range references {
				if strings.Contains(reference, ":") || strings.Contains(reference, "/") {
					t.Fatalf("remote URI remained: %s", reference)
				}
				if _, err := os.Stat(filepath.Join(directory, reference)); err != nil {
					t.Fatalf("bundle resource missing: %s", reference)
				}
			}
		}
	}
	if !strings.Contains(combined, "#EXT-X-BYTERANGE:4@4") || !strings.Contains(combined, "#EXT-X-DISCONTINUITY") ||
		!strings.Contains(combined, "IV=0x00000000000000000000000000000001") {
		t.Fatal("lost HLS timeline/encryption metadata")
	}
	keys, _ := filepath.Glob(filepath.Join(directory, "*.key"))
	if len(keys) != 1 {
		t.Fatalf("missing key: %v", keys)
	}
	key, _ := os.ReadFile(keys[0])
	if string(key) != "0123456789abcdef" {
		t.Fatal("wrong key")
	}
}

func TestNativeDownloadHLSRejectsLiveAndUnsupportedEncryption(t *testing.T) {
	manager := downloadTestManager(t)
	for _, playlist := range []string{
		"#EXTM3U\n#EXTINF:2,\nseg.ts\n",
		"#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"\n#EXTINF:2,\nseg.ts\n#EXT-X-ENDLIST\n",
		"#EXTM3U\n#EXT-X-DEFINE:NAME=\"host\",VALUE=\"x\"\n#EXTINF:2,\nseg.ts\n#EXT-X-ENDLIST\n",
	} {
		_, err := manager.downloadBundle(context.Background(), providerMedia{URL: "https://synthetic.test/index.m3u8", Playlist: playlist}, 0)
		if err == nil {
			t.Fatalf("accepted unsupported playlist: %s", playlist)
		}
	}
}
