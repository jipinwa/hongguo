package core

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveProviderCatalogRankingDetailAndPlaybackSmoke(t *testing.T) {
	if os.Getenv("CHECK_LIVE_PROVIDERS") != "true" {
		t.Skip("set CHECK_LIVE_PROVIDERS=true to touch live provider text APIs")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	for _, scenario := range []struct {
		source string
		board  string
	}{
		{source: sourceHuangju, board: "huangju-hot"},
		{source: sourceYeguo, board: "yeguo-recommend"},
		{source: sourceDSD, board: "dsd-catalog"},
	} {
		t.Run(scenario.source, func(t *testing.T) {
			dramas, more, err := fetchLiveCatalogPage(ctx, engine.downloader, scenario.source)
			if err != nil || len(dramas) == 0 {
				t.Fatalf("catalog failed: count=%d more=%t err=%v", len(dramas), more, err)
			}
			board, found := findRankingBoard(scenario.board)
			if !found {
				t.Fatal("ranking board missing")
			}
			ranking, err := engine.downloader.loadRankingPage(ctx, board, 1, true)
			if err != nil || len(ranking.Items) == 0 {
				t.Fatalf("ranking failed: count=%d err=%v", len(ranking.Items), err)
			}
			var lastErr error
			for index, drama := range dramas {
				if index >= 8 {
					break
				}
				raw, chapters, err := fetchLiveDetail(ctx, engine.downloader, scenario.source, drama.SourceID)
				if err != nil {
					lastErr = err
					continue
				}
				if raw.ID != drama.ID || len(chapters) == 0 {
					lastErr = err
					continue
				}
				for chapterIndex, chapter := range chapters {
					if chapterIndex >= 4 {
						break
					}
					media, err := engine.downloader.resolveProviderMedia(ctx, Task{DramaID: raw.ID, DramaTitle: raw.DisplayTitle(), Chapter: chapter, Index: chapterIndex + 1})
					if err != nil {
						lastErr = err
						continue
					}
					choice := nativePlaybackChoices(media, 0)
					if len(choice.media) == 0 {
						lastErr = err
						continue
					}
					plan, err := engine.nativeOpenPlayback(ctx, choice)
					if err != nil {
						lastErr = err
						continue
					}
					engine.nativeReleasePlayback(plan.Session)
					if !strings.HasPrefix(plan.URL, "http://127.0.0.1:") || plan.RouteCount < 1 {
						t.Fatalf("playback plan did not use local stream route: %+v", plan)
					}
					t.Logf("%s live smoke ok: catalog=%d ranking=%d detail=%s chapters=%d routes=%d quality=%d", scenario.source, len(dramas), len(ranking.Items), raw.ID, len(chapters), plan.RouteCount, plan.Quality)
					return
				}
			}
			t.Fatalf("could not resolve a playable %s episode; last error: %v", scenario.source, lastErr)
		})
	}
}

func fetchLiveCatalogPage(ctx context.Context, d *Downloader, source string) ([]Drama, bool, error) {
	switch source {
	case sourceHuangju:
		return d.fetchHuangjuCatalogPage(ctx, 1, "", "")
	case sourceYeguo:
		return d.fetchYeguoCatalogPage(ctx, 1, "", "")
	case sourceDSD:
		return d.fetchDSDCatalogPage(ctx, 1, "", "")
	default:
		return nil, false, errNativeBuildSource
	}
}

func fetchLiveDetail(ctx context.Context, d *Downloader, source, sourceID string) (Drama, []Chapter, error) {
	switch source {
	case sourceHuangju:
		return d.fetchHuangjuDetail(ctx, sourceID)
	case sourceYeguo:
		return d.fetchYeguoDetail(ctx, sourceID)
	case sourceDSD:
		return d.fetchDSDDetail(ctx, sourceID)
	default:
		return Drama{}, nil, errNativeBuildSource
	}
}
