package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type sourceFixtureTransport func(*http.Request) (*http.Response, error)

func (transport sourceFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func sourceFixtureResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: request}
}

func sourceFixtureDownloader(t *testing.T, transport sourceFixtureTransport) *Downloader {
	t.Helper()
	return &Downloader{cfg: Config{dataDir: t.TempDir(), Retries: 1, PageSize: 30, MaxPagesPerSort: 1},
		client: &http.Client{Transport: transport}, limiter: newRequestLimiter(3, 0), providerHosts: map[string]string{}}
}

func sourceFixtureEngine(t *testing.T, transport sourceFixtureTransport) *nativeEngine {
	t.Helper()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.client.Transport = transport
	engine.downloader.limiter = newRequestLimiter(3, 0)
	engine.downloader.cfg.Retries = 1
	return engine
}

func awaitSourceTask(t *testing.T, engine *nativeEngine, source string) nativeSourceStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := engine.sourceStatus(source)
		if !status.Running {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("source task did not finish")
	return nativeSourceStatus{}
}

func TestSourceUpdatesPreservePagesOtherSourcesAndMetadata(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if !strings.Contains(request.URL.Path, "/api/videos/category/") {
			return nil, errors.New("only catalog text is allowed")
		}
		id := "new"
		if request.URL.Query().Get("page") == "3" {
			id = "continued"
		}
		return sourceFixtureResponse(request, 200, fmt.Sprintf(`{"data":[{"id":%q,"title":"合成短剧","desc":"完整资料","totalEpisode":2}]}`, id)), nil
	})
	engine.catalogs[sourceHuangguoAI] = []nativeDrama{{ID: "huangguoai:old", Source: sourceHuangguoAI, Title: "原有短剧", Description: "保留资料", Episodes: 2}}
	engine.catalogStates[sourceHuangguoAI] = nativeCatalogState{Page: 2, HasMore: true}
	engine.catalogs[sourceHongguo] = []nativeDrama{{ID: "hongguo:1", Source: sourceHongguo, Title: "其他站源"}}
	if err := engine.updateSource(context.Background(), sourceHuangguoAI, "update"); err != nil {
		t.Fatal(err)
	}
	page := engine.nativeCached(sourceHuangguoAI)
	if len(page.Items) != 3 || page.Page != 3 || page.HasMore {
		t.Fatalf("lost refresh/tail state: %+v", page)
	}
	if len(engine.nativeCached(sourceHongguo).Items) != 1 {
		t.Fatal("updated another source")
	}
	engine.saveCatalogCache(sourceHuangguoAI, &nativeCatalogResult{Items: []nativeDrama{{ID: "huangguoai:old", Title: "新名称"}}, Page: 1, HasMore: true})
	page = engine.nativeCached(sourceHuangguoAI)
	if page.Page != 3 || page.HasMore || len(page.Items) != 3 || page.Items[0].Description != "保留资料" {
		t.Fatal("head refresh discarded cached metadata or pagination", page)
	}
}

func TestYeguoSourceUpdateBatchesConfiguredPagesWithPost(t *testing.T) {
	var pages []string
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.yeguo.test" || request.URL.Path != "/api/theater/exploreList" {
			t.Fatalf("unexpected yeguo catalog request: %s", request.URL.String())
		}
		if request.Method != http.MethodPost {
			t.Fatalf("yeguo catalog must use POST for pagination, got %s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		page := request.Form.Get("page")
		pages = append(pages, page)
		id := "90" + page
		body := fmt.Sprintf(`{"status":"1","data":{"list":[{"video_id":%q,"title":"野果分页%s","description":"完整资料","episode_count":"6","serialize_status":"2"}],"page":%s,"limit":20,"total":60,"has_more":%q}}`, id, page, page, map[bool]string{true: "1", false: "0"}[page != "3"])
		return sourceFixtureResponse(request, http.StatusOK, body), nil
	})
	engine.downloader.cfg.MaxPagesPerSort = 3
	engine.downloader.yeguoClient().access = &yeguoAccess{base: "https://api.yeguo.test", identifier: "fixture-trace", loadedAt: time.Now()}

	if err := engine.updateSource(context.Background(), sourceYeguo, "update"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(pages, ",") != "1,2,3" {
		t.Fatalf("yeguo update did not batch configured pages: %v", pages)
	}
	page := engine.nativeCached(sourceYeguo)
	if len(page.Items) != 3 || page.Page != 3 || page.HasMore {
		t.Fatalf("wrong yeguo source cache after update: %+v", page)
	}
}

func TestSourceJobsDeduplicateCancelAndRecoverAfterRestart(t *testing.T) {
	if buildAllSources != "true" {
		t.Skip("full edition source lifecycle")
	}
	var calls atomic.Int32
	started := make(chan struct{})
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	engine.catalogs[sourceHuangguoVideo] = []nativeDrama{{ID: "huangguo-video:video/fixture", Source: sourceHuangguoVideo, Title: "已有缓存"}}
	if _, err := engine.startSourceTask(sourceHuangguoVideo, "update", nativeDrama{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := engine.startSourceTask(sourceHuangguoVideo, "check", nativeDrama{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || engine.sourceStatus(sourceHuangguoVideo).Operation != "update" {
		t.Fatal("duplicate request replaced the running task")
	}
	if count, _ := engine.workLease("", ""); count != 1 {
		t.Fatal("background service cannot see source work")
	}
	engine.cancelSourceTask(sourceHuangguoVideo)
	status := awaitSourceTask(t, engine, sourceHuangguoVideo)
	if status.Count != 1 || status.Stage != "已停止" {
		t.Fatal("cancel discarded cache or did not stop", status)
	}
	engine.changeSourceRecord(sourceHuangguoVideo, func(record *nativeSourceRecord) { record.Running = true })
	restored := &nativeEngine{directory: engine.directory}
	restored.loadSourceRecords()
	if record := restored.sourceRecords[sourceHuangguoVideo]; record.Running || record.Error == "" {
		t.Fatal("interrupted work stayed running")
	}
}

func TestSourceHealthRejectsHTML200AndPreservesFailureStage(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/videos":
			return sourceFixtureResponse(request, 200, `<article class="video-card"><a href="/video/fixture" title="合成检测剧">合成检测剧</a></article>`), nil
		case "/video/fixture":
			return sourceFixtureResponse(request, 200, `<h1>合成检测剧</h1><div data-hls="https://media.example.test/master.m3u8"></div>`), nil
		case "/master.m3u8":
			response := sourceFixtureResponse(request, 200, `<html><title>Just a moment</title><script>_cf_chl_opt={}</script>Cloudflare</html>`)
			response.Header.Set("Content-Type", "text/html")
			return response, nil
		default:
			t.Error("unexpected resource, including images", request.URL.Path)
			return nil, errors.New("blocked")
		}
	})
	err := engine.checkSource(context.Background(), sourceHuangguoVideo, nativeDrama{}, true)
	status := engine.sourceStatus(sourceHuangguoVideo)
	if err == nil || status.Health == nil || status.Health.State != "failed" || len(status.Health.Steps) != 3 || status.Health.Steps[2].State != "failed" {
		t.Fatal("HTML 200 was reported as playable", err, status.Health)
	}
	if status.Health.Steps[2].HTTPStatus != 200 || !strings.Contains(status.Health.Steps[2].Message, "浏览器验证") {
		t.Fatal("missing CF failure detail", status.Health.Steps)
	}
}

func TestSourceHealthChecksHLSKeyAndBoundedMedia(t *testing.T) {
	var segments atomic.Int32
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/videos":
			return sourceFixtureResponse(request, 200, `<article class="video-card"><a href="/video/fixture" title="合成检测剧">合成检测剧</a></article>`), nil
		case "/video/fixture":
			return sourceFixtureResponse(request, 200, `<div data-hls="https://huangguo.video/master.m3u8"></div>`), nil
		case "/master.m3u8":
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-STREAM-INF:RESOLUTION=320x480\nmedia.m3u8?signature=a%2Fb\n"), nil
		case "/media.m3u8":
			if request.URL.RawQuery != "signature=a%2Fb" {
				t.Error("signed media URL changed")
			}
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/api/hls_key/fixture\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST"), nil
		case "/api/preview-token":
			return sourceFixtureResponse(request, 200, `{"ok":true,"token":"fixture-preview","expires_in":300}`), nil
		case "/api/hls_key/fixture":
			if request.Header.Get("X-Preview-Token") != "fixture-preview" {
				t.Error("missing preview token")
			}
			return sourceFixtureResponse(request, 200, "0123456789abcdef"), nil
		case "/segment.ts":
			segments.Add(1)
			if request.Header.Get("Range") != "bytes=0-1023" || request.Header.Get("X-Preview-Token") != "" {
				t.Error("unbounded probe or leaked token")
			}
			return sourceFixtureResponse(request, 206, strings.Repeat("s", 2048)), nil
		default:
			t.Error("unexpected resource", request.URL.Path)
			return nil, errors.New("blocked")
		}
	})
	if err := engine.checkSource(context.Background(), sourceHuangguoVideo, nativeDrama{}, true); err != nil {
		t.Fatal(err)
	}
	health := engine.sourceStatus(sourceHuangguoVideo).Health
	if health.State != "ok" || len(health.Steps) != 5 || segments.Load() != 1 {
		t.Fatal("incomplete health report", health)
	}
	body, err := os.ReadFile(filepath.Join(engine.directory, "sources.json"))
	if err != nil || strings.Contains(string(body), "fixture-preview") {
		t.Fatal("health persistence leaked preview token", err)
	}
	var saved map[string]nativeSourceRecord
	if json.Unmarshal(body, &saved) != nil || saved[sourceHuangguoVideo].Health == nil {
		t.Fatal("health report was not persisted")
	}
}

func TestSourceBackoffUsesRelativeTimeAndRetainsTypedError(t *testing.T) {
	backoff := &requestBackoff{host: "huangguo.video", status: 403, until: time.Now().Add(time.Minute)}
	if !strings.Contains(backoff.Error(), "秒后") || strings.Contains(backoff.Error(), backoff.until.Format("15:04:05")) {
		t.Fatal("retry time depends on native timezone")
	}
	var restored *requestBackoff
	if !errors.As(publicError(fmt.Errorf("playlist: %w", backoff)), &restored) {
		t.Fatal("redaction discarded structured retry time")
	}
}
