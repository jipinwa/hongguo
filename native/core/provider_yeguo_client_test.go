package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestYeguoAPIClientUsesRouteMethods(t *testing.T) {
	var sawCategories, sawCatalog, sawSearch, sawPlay bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		switch request.URL.Path {
		case "/api/home/contentOptions":
			sawCategories = true
			if request.Method != http.MethodGet {
				t.Errorf("categories route used %s", request.Method)
			}
		case "/api/theater/exploreList":
			sawCatalog = true
			if request.Method != http.MethodPost {
				t.Errorf("catalog route used %s", request.Method)
			}
			if request.URL.RawQuery != "" || request.Form.Get("page") != "2" {
				t.Errorf("catalog query/form mismatch: query=%q form=%q", request.URL.RawQuery, request.PostForm.Encode())
			}
		case "/api/search/result":
			sawSearch = true
			if request.Method != http.MethodPost {
				t.Errorf("search route used %s", request.Method)
			}
			if request.URL.RawQuery != "" || request.Form.Get("keyword") != "样本" || request.Form.Get("page") != "3" {
				t.Errorf("search query/form mismatch: query=%q form=%q", request.URL.RawQuery, request.Form.Encode())
			}
		case "/api/playlet/play":
			sawPlay = true
			if request.Method != http.MethodPost {
				t.Errorf("play route used %s", request.Method)
			}
			if request.URL.RawQuery != "" || request.Form.Get("episode_id") != "456" {
				t.Errorf("play query/form mismatch: query=%q form=%q", request.URL.RawQuery, request.Form.Encode())
			}
		default:
			t.Errorf("unexpected route: %s", request.URL.Path)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"status":"1","data":{"ok":"1"}}`)
	}))
	defer server.Close()

	client := &yeguoAPIClient{
		downloader: &Downloader{client: server.Client()},
		site:       yeguoBaseURL,
		access:     &yeguoAccess{base: server.URL, identifier: "fixture-trace", loadedAt: time.Now()},
	}
	if _, err := client.call(context.Background(), "/api/home/contentOptions", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(context.Background(), "/api/theater/exploreList", url.Values{"page": {"2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(context.Background(), "/api/search/result", url.Values{"keyword": {"样本"}, "page": {"3"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.call(context.Background(), "/api/playlet/play", url.Values{"episode_id": {"456"}}); err != nil {
		t.Fatal(err)
	}
	if !sawCategories || !sawCatalog || !sawSearch || !sawPlay {
		t.Fatalf("missing yeguo routes: categories=%t catalog=%t search=%t play=%t", sawCategories, sawCatalog, sawSearch, sawPlay)
	}
}

func TestResolveYeguoMediaKeepsCodecAlternatives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/playlet/play" {
			t.Fatalf("unexpected route: %s", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if request.Form.Get("playlet_id") != "123" || request.Form.Get("episode_id") != "456" {
			t.Fatalf("wrong yeguo playback request: %s", request.Form.Encode())
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"status":"1","data":{"playlet_id":"123","id":"456","episode_sort":"7","episode_duration":"12.5","resolution":"1920x1080","video_url":"https://media.example.test/h264.mp4","video_url_h265":"https://media.example.test/h265.mp4"}}`)
	}))
	defer server.Close()

	downloader := &Downloader{}
	client := downloader.yeguoClient()
	client.access = &yeguoAccess{base: server.URL, identifier: "fixture-trace", loadedAt: time.Now()}
	media, err := downloader.resolveYeguoMedia(context.Background(), Task{
		DramaID: providerDramaID(sourceYeguo, "123"),
		Chapter: Chapter{
			ID: providerChapterID(sourceYeguo, "123", "456"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://media.example.test/h264.mp4" || len(media.Variants) != 2 ||
		media.Variants[0].URL != "https://media.example.test/h264.mp4" ||
		media.Variants[1].URL != "https://media.example.test/h265.mp4" {
		t.Fatalf("yeguo alternatives were not preserved: %+v", media)
	}
	choices := nativePlaybackChoices(media, 0)
	if len(choices.media) != 2 {
		t.Fatalf("playback choices lost yeguo alternatives: %+v", choices.media)
	}
}
