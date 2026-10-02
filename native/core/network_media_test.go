package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHuangguoMediaUsesBrowserSessionPageRefererAndPreviewToken(t *testing.T) {
	var previews, keys atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || !strings.Contains(r.UserAgent(), "Chrome/150.") {
			t.Error("media lost browser TLS/HTTP2 client")
		}
		if r.URL.Path == "/video/fixture" {
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "session", Path: "/", Secure: true})
			io.WriteString(w, `<h1>合成分集</h1><div data-hls="/master.m3u8?signature=a%2Fb"></div>`)
			return
		}
		if r.Header.Get("Referer") != "https://"+r.Host+"/video/fixture" || r.Header.Get("Sec-Fetch-Mode") != "cors" || r.Header.Get("Sec-Fetch-Dest") != "empty" || r.Header.Get("Upgrade-Insecure-Requests") != "" {
			t.Error("media was sent as navigation or lost its exact page referer")
			w.WriteHeader(403)
			return
		}
		if cookie, err := r.Cookie("fixture"); err != nil || cookie.Value != "session" {
			t.Error("browser session lost")
		}
		switch r.URL.Path {
		case "/master.m3u8":
			if r.URL.RawQuery != "signature=a%2Fb" {
				t.Error("signed media query changed")
			}
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/api/hls_key/test\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/api/preview-token":
			previews.Add(1)
			io.WriteString(w, `{"ok":true,"token":"local-fixture-token","expires_in":300}`)
		case "/api/hls_key/test":
			keys.Add(1)
			if r.Header.Get("X-Preview-Token") != "local-fixture-token" || r.Header.Get("Origin") != "https://"+r.Host {
				t.Error("key lost its token or Origin")
			}
			io.WriteString(w, "0123456789abcdef")
		case "/segment.ts":
			if r.Header.Get("X-Preview-Token") != "" {
				t.Error("token leaked to segment")
			}
			io.WriteString(w, "synthetic-media-only")
		default:
			t.Error("unexpected source resource", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	d := sourceFixtureDownloader(t, nil)
	d.cfg.HuangguoVideoURL, d.cfg.InsecureTLS = server.URL, true
	d.proxyRouter = &proxyRouter{}
	d.client.Transport = newHuangguoBrowserTransport(http.DefaultTransport, d)
	t.Cleanup(d.client.CloseIdleConnections)
	task := Task{DramaID: "huangguo-video:video/fixture", Chapter: Chapter{ID: "fixture", Source: sourceHuangguoVideo, PageURL: "https://huangguo.video/video/fixture"}, Index: 1}
	media, err := d.resolveProviderMedia(context.Background(), task)
	if err != nil || media.Referer != server.URL+"/video/fixture" || media.URL != server.URL+"/master.m3u8?signature=a%2Fb" {
		t.Fatal("media resolve failed", err, media.URL, media.Referer)
	}
	stream, err := newNativeStreamServer(d)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.server.Close()
	address, token := stream.nativeOpen(media)
	defer stream.nativeRelease(token)
	read := func(address string) string {
		t.Helper()
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatal("stream error", response.StatusCode, string(body))
		}
		return string(body)
	}
	playlist := read(address)
	match := nativePlaylistURI.FindStringSubmatch(playlist)
	if len(match) != 2 || read(match[1]) != "0123456789abcdef" {
		t.Fatal("player did not receive source key")
	}
	engine := &nativeEngine{downloader: d, directory: d.cfg.dataDir}
	manager := newNativeDownloads(engine)
	engine.downloads = manager
	t.Cleanup(manager.close)
	job := nativeDownloadJob{ID: "synthetic-job", Drama: nativeDrama{ID: task.DramaID, Source: sourceHuangguoVideo}, Index: 1}
	result, err := manager.transferMedia(context.Background(), job, media)
	if err != nil || result.file != "index.m3u8" {
		t.Fatal("download did not reuse media chain", err)
	}
	if previews.Load() != 1 || keys.Load() != 2 {
		t.Fatal("preview session not reused across player and download", previews.Load(), keys.Load())
	}
}

func TestHuangguoPreviewTokenRefreshIsBoundedAndScoped(t *testing.T) {
	var previews, keys atomic.Int32
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/preview-token":
			previews.Add(1)
			return sourceFixtureResponse(request, 200, `{"ok":true,"token":"fresh-fixture","expires_in":300}`), nil
		case "/api/hls_key/test":
			keys.Add(1)
			if request.URL.Hostname() == "other.example" {
				if request.Header.Get("X-Preview-Token") != "" {
					t.Error("preview token leaked to another host")
				}
				return sourceFixtureResponse(request, 200, "0123456789abcdef"), nil
			}
			if keys.Load() == 1 {
				return sourceFixtureResponse(request, 403, `{"error":"expired token"}`), nil
			}
			return sourceFixtureResponse(request, 200, "0123456789abcdef"), nil
		}
		return nil, errors.New("unexpected resource")
	})
	for _, host := range []string{"huangguo.video", "other.example"} {
		request, _ := http.NewRequest(http.MethodGet, "https://"+host+"/api/hls_key/test", nil)
		request.Header.Set("Referer", "https://huangguo.video/video/fixture")
		response, err := d.doMediaRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if previews.Load() != 2 || keys.Load() != 3 {
		t.Fatal("unexpected token renewal count", previews.Load(), keys.Load())
	}
}

func TestLegacyPlaybackAndDownloadUseAuthenticatedPlaylistAndSourceKey(t *testing.T) {
	var keys atomic.Int32
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/app/vid/h5/m3u8/opaque-video":
			if request.URL.Query().Get("token") != "fixture-session" || request.URL.Query().Get("c") != defaultCDNURL {
				t.Error("legacy playlist lost its own session")
			}
			return sourceFixtureResponse(request, 200, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"/api/app/vid/sec\"\n#EXTINF:4,\nsegment.ts\n#EXT-X-ENDLIST\n"), nil
		case "/api/app/vid/sec":
			keys.Add(1)
			if request.Header.Get("Origin") != legacyFrontendURL || request.Header.Get("Referer") != legacyFrontendURL+"/" {
				t.Error("legacy key lost source headers")
			}
			return sourceFixtureResponse(request, 200, "0123456789abcdef"), nil
		case "/api/app/vid/h5/m3u8/segment.ts":
			return sourceFixtureResponse(request, 200, "synthetic-segment"), nil
		default:
			t.Error("unexpected legacy resource", request.URL.Path)
			return nil, errors.New("blocked")
		}
	})
	d.cfg.APIBase = "https://legacy.example.test"
	d.cfg.Token = "fixture-session"
	d.cfg.InterfaceKey, d.cfg.ParamKey, d.cfg.ParamIV = fixtureLegacyProtocol.InterfaceKey, fixtureLegacyProtocol.ParamKey, fixtureLegacyProtocol.ParamIV
	media, err := d.resolveProviderMedia(context.Background(), Task{DramaID: "cloudfront:fixture", Chapter: Chapter{Source: sourceCloudFront, VideoURL: "opaque-video"}})
	if err != nil || media.Duration.Seconds() != 4 {
		t.Fatal("legacy resolver failed", err)
	}
	engine := &nativeEngine{downloader: d, directory: d.cfg.dataDir}
	manager := newNativeDownloads(engine)
	engine.downloads = manager
	t.Cleanup(manager.close)
	result, err := manager.transferMedia(context.Background(), nativeDownloadJob{ID: "legacy-fixture"}, media)
	if err != nil || result.file != "index.m3u8" || keys.Load() != 1 {
		t.Fatal("legacy download failed", err, keys.Load())
	}
	if _, err := os.Stat(filepath.Join(manager.root, "legacy-fixture", "index.m3u8")); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyDiscoverySkipsFailedHost(t *testing.T) {
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/app/ping/check" {
			return nil, errors.New("unexpected request")
		}
		return sourceFixtureResponse(request, 200, `{"code":200}`), nil
	})
	first, err := d.apiEndpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.resetAPIEndpoint(first)
	next, err := d.apiEndpoint(context.Background())
	if err != nil || next == first {
		t.Fatal("failed host was immediately reselected", next, err)
	}
	var wrapped legacyTabList
	if json.Unmarshal([]byte(`{"list":[{"id":"tab","name":"分类"}]}`), &wrapped) != nil || len(wrapped) != 1 {
		t.Fatal("tab format unsupported")
	}
}

func TestVideoEpisodeParsingPrefersNumberedLinksOverStartButton(t *testing.T) {
	body := `<a href="/video/two">开始观看</a><a href="/video/one"><p>第1集</p></a><a href="/video/two"><p>第2集</p></a>`
	episodes := parseHuangguoVideoEpisodes(body, "https://huangguo.video/series/synthetic")
	if len(episodes) != 2 || episodes[0].Key != "video/one" || episodes[1].Index != 2 {
		t.Fatal("start button replaced an episode", episodes)
	}
}
