package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nativeHongguoRequest struct {
	Offset  int    `json:"offset"`
	Session string `json:"session_id"`
	Scene   string `json:"req_scene"`
}

func nativeHongguoRequestForTest(t *testing.T, request *http.Request) nativeHongguoRequest {
	t.Helper()
	var payload nativeHongguoRequest
	if request.URL.Path != "/reading/distribution/category/landpage/v/" || json.NewDecoder(request.Body).Decode(&payload) != nil {
		t.Errorf("unexpected request, only synthetic catalog text is allowed: %s", request.URL.Path)
	}
	return payload
}

func nativeHongguoResponse(request *http.Request, next int, more bool, session string, ids ...string) *http.Response {
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"series_id": id, "series_title": "合成目录 " + id})
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]any{
		"next_offset": next, "has_more": more, "session_id": session, "video_data": rows,
	}})
	return sourceFixtureResponse(request, http.StatusOK, string(body))
}

func reopenCatalogEngine(t *testing.T, directory string, transport sourceFixtureTransport) *nativeEngine {
	t.Helper()
	engine, err := newNativeEngine(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.client.Transport = transport
	engine.downloader.limiter = newRequestLimiter(3, 0)
	engine.downloader.cfg.Retries = 1
	return engine
}

func TestNativeHongguoCursorRestartsPerCatalog(t *testing.T) {
	var calls []nativeHongguoRequest
	var device, install string
	transport := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		payload := nativeHongguoRequestForTest(t, request)
		calls = append(calls, payload)
		if device == "" {
			device, install = request.URL.Query().Get("device_id"), request.URL.Query().Get("iid")
		} else if request.URL.Query().Get("device_id") != device || request.URL.Query().Get("iid") != install {
			t.Error("restart changed the catalog device identity")
		}
		if payload.Offset > 0 && payload.Session != "fixture-"+payload.Scene {
			t.Error("restart lost the feed session", payload)
		}
		base := map[string]int{"default": 1000, "comic_series": 2000, "ai_series": 3000}[payload.Scene]
		position := payload.Offset / 18
		return nativeHongguoResponse(request, payload.Offset+18, true, "fixture-"+payload.Scene,
			strconv.Itoa(base+position+1), strconv.Itoa(base+position+2)), nil
	})
	engine := sourceFixtureEngine(t, transport)
	for _, input := range []nativeInput{
		{Source: sourceHongguo, Page: 1},
		{Source: sourceHongguo, Category: "short_play", Page: 1},
		{Source: sourceHongguo, Category: "comic_series", Page: 1},
		{Source: sourceHongguo, Category: "short_play", Page: 2},
	} {
		if result, err := engine.nativeCatalog(context.Background(), input); err != nil || result.Warning != "" {
			t.Fatal("could not save the initial catalogs", err, result.Warning)
		}
	}
	expected, _ := json.Marshal(engine.downloader.hongguoCatalogSnapshot())
	engine = reopenCatalogEngine(t, engine.directory, transport)
	restored, _ := json.Marshal(engine.downloader.hongguoCatalogSnapshot())
	if !bytes.Equal(expected, restored) {
		t.Fatal("restart lost cursor fields, signatures or device state")
	}
	calls = nil
	for _, input := range []nativeInput{
		{Source: sourceHongguo, Page: 2},
		{Source: sourceHongguo, Category: "short_play", Page: 3},
		{Source: sourceHongguo, Category: "comic_series", Page: 2},
	} {
		if result, err := engine.nativeCatalog(context.Background(), input); err != nil || result.Warning != "" {
			t.Fatal("could not resume the saved catalog", err, result.Warning)
		}
	}
	var offsets []int
	for _, call := range calls {
		offsets = append(offsets, call.Offset)
	}
	if !reflect.DeepEqual(offsets, []int{18, 18, 18, 36, 18}) {
		t.Fatal("total catalog and categories shared or reset their cursors", offsets)
	}
	for key, items := range engine.catalogs {
		seen := map[string]bool{}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatal("overlapping pages duplicated an ID", key, item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(engine.catalogs[sourceHongguo]) != 10 || len(engine.catalogs["hongguo|short_play"]) != 4 {
		t.Fatal("resuming a category dropped entries from the shared library")
	}
	if _, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHongguo, Page: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	if engine.downloader.hongguoCatalogSnapshot().Feeds["short_play"].Offset != 36 {
		t.Fatal("refreshing an unchanged head reset the saved continuation")
	}
}

func TestNativeHongguoCursorRecoversSessionsAfterRestart(t *testing.T) {
	for _, scenario := range []string{"expired", "rejected", "repeated-page"} {
		t.Run(scenario, func(t *testing.T) {
			var calls []nativeHongguoRequest
			transport := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
				payload := nativeHongguoRequestForTest(t, request)
				calls = append(calls, payload)
				if payload.Offset == 0 {
					return nativeHongguoResponse(request, 18, true, "old-session", "700001", "700002"), nil
				}
				if payload.Offset != 18 {
					t.Error("session recovery changed the saved offset", payload.Offset)
				}
				if scenario == "expired" && payload.Session != "" {
					t.Error("reused an expired session")
				}
				if payload.Session != "" {
					if scenario == "rejected" {
						return sourceFixtureResponse(request, 200, `{"code":1001}`), nil
					}
					return nativeHongguoResponse(request, 36, true, "old-session", "700002", "700001"), nil
				}
				return nativeHongguoResponse(request, 36, true, "new-session", "700002", "700003"), nil
			})
			engine := sourceFixtureEngine(t, transport)
			input := nativeInput{Source: sourceHongguo, Category: "ai_series", Page: 1}
			if result, err := engine.nativeCatalog(context.Background(), input); err != nil || result.Warning != "" {
				t.Fatal(err, result.Warning)
			}
			if scenario == "expired" {
				engine.mu.Lock()
				cursor := engine.hongguoCatalog.Feeds["category:ai_series"]
				cursor.UpdatedAt = time.Now().Add(-time.Hour)
				engine.hongguoCatalog.Feeds["category:ai_series"] = cursor
				err := engine.writeCatalogDiskLocked()
				engine.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			engine = reopenCatalogEngine(t, engine.directory, transport)
			input.Page = 2
			if result, err := engine.nativeCatalog(context.Background(), input); err != nil || result.Warning != "" {
				t.Fatal("failed to recover a saved session", err, result.Warning)
			}
			wantCalls := 3
			if scenario == "expired" {
				wantCalls = 2
			}
			if len(calls) != wantCalls || calls[len(calls)-1].Session != "" {
				t.Fatal("session recovery was not bounded", calls)
			}
			restarted := reopenCatalogEngine(t, engine.directory, transport)
			cursor := restarted.downloader.hongguoCatalogSnapshot().Feeds["category:ai_series"]
			if cursor.Offset != 36 || cursor.SessionID != "new-session" || len(restarted.catalogs["hongguo|ai_series"]) != 3 {
				t.Fatal("recovered items and cursor were not saved together")
			}
		})
	}
}

func TestNativeHongguoStalledCursorKeepsPartialItems(t *testing.T) {
	var calls []int
	transport := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		payload := nativeHongguoRequestForTest(t, request)
		calls = append(calls, payload.Offset)
		if payload.Offset == 0 {
			return nativeHongguoResponse(request, 18, true, "fixture", "700001"), nil
		}
		return nativeHongguoResponse(request, 18, true, "fixture", "700002"), nil
	})
	engine := sourceFixtureEngine(t, transport)
	input := nativeInput{Source: sourceHongguo, Category: "ai_series", Page: 1}
	if _, err := engine.nativeCatalog(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.Page = 2
	result, err := engine.nativeCatalog(context.Background(), input)
	if err != nil || result.Warning == "" || result.Fresh || !reflect.DeepEqual(calls, []int{0, 18, 18}) {
		t.Fatal("stalled pagination was accepted or retried indefinitely", err, result.Warning, calls)
	}
	restarted := reopenCatalogEngine(t, engine.directory, transport)
	cached := restarted.nativeCached("hongguo|ai_series")
	cursor := restarted.downloader.hongguoCatalogSnapshot().Feeds["category:ai_series"]
	if cursor.Offset != 18 || len(cached.Items) != 2 || cached.Fresh || cached.Warning == "" {
		t.Fatal("restart lost the last valid cursor, partial data or failure warning")
	}
}

func TestNativeCatalogPreservesLargeLibraryAndPage501(t *testing.T) {
	var requested string
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/videos/category/ai-duanju" {
			return nil, errors.New("only synthetic catalog metadata is allowed")
		}
		requested = request.URL.Query().Get("page")
		return sourceFixtureResponse(request, 200, `{"data":[{"id":"6001","title":"第6001部"}]}`), nil
	})
	items := make([]nativeDrama, 6000)
	for index := range items {
		items[index] = nativeDrama{ID: fmt.Sprintf("huangguoai:%d", index+1), Source: sourceHuangguoAI, Title: "合成目录"}
	}
	key := nativeCatalogKey(sourceHuangguoAI, "ai-duanju")
	if err := engine.saveCatalogCache(key, &nativeCatalogResult{Items: items, Page: 500, HasMore: true}); err != nil {
		t.Fatal(err)
	}
	if result, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHuangguoAI, Category: "ai-duanju", Page: 501}); err != nil || result.Warning != "" {
		t.Fatal(err, result.Warning)
	}
	if requested != "501" {
		t.Fatal("catalog silently clamped the requested page", requested)
	}
	if err := engine.saveCatalogCache(nativeCatalogKey(sourceHuangguoAI, "ai-manju"), &nativeCatalogResult{
		Items: []nativeDrama{{ID: "huangguoai:6001", Source: sourceHuangguoAI, Title: "第6001部"}, {ID: "huangguoai:6002", Source: sourceHuangguoAI, Title: "第6002部"}},
		Page:  1,
	}); err != nil {
		t.Fatal(err)
	}
	restarted := reopenCatalogEngine(t, engine.directory, nil)
	cached := restarted.nativeCached(key)
	if len(cached.Items) != 6001 || cached.Page != 501 || cached.HasMore || len(restarted.catalogs[sourceHuangguoAI]) != 6002 {
		t.Fatal("restart truncated data, advanced past lost IDs or duplicated category entries", len(cached.Items), cached.Page)
	}
}

func TestNativeCatalogSaveFailureRetriesWithoutAdvancing(t *testing.T) {
	var offsets []int
	transport := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		payload := nativeHongguoRequestForTest(t, request)
		offsets = append(offsets, payload.Offset)
		position := payload.Offset / 18
		return nativeHongguoResponse(request, payload.Offset+18, true, "fixture",
			strconv.Itoa(1001+position), strconv.Itoa(1002+position)), nil
	})
	engine := sourceFixtureEngine(t, transport)
	input := nativeInput{Source: sourceHongguo, Category: "short_play", Page: 1}
	if _, err := engine.nativeCatalog(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(engine.directory, "catalogs.json")
	original, _ := os.ReadFile(path)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	input.Page = 2
	result, err := engine.nativeCatalog(context.Background(), input)
	if err != nil || result.Warning == "" || result.Fresh || result.saveError == nil {
		t.Fatal("save failure was reported as a fresh catalog", err, result.Warning)
	}
	if len(engine.nativeCached("hongguo|short_play").Items) != 3 {
		t.Fatal("save failure discarded usable memory data")
	}
	input.Page = 3
	if result, err := engine.nativeCatalog(context.Background(), input); err != nil || result.Warning == "" || len(offsets) != 2 {
		t.Fatal("an unsaved catalog advanced to another page", err, offsets)
	}
	backup, _ := os.ReadFile(path + ".saved")
	if !bytes.Equal(original, backup) {
		t.Fatal("failed writes changed the previous persisted catalog")
	}
	if err := os.Rename(path, path+".blocked"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	restarted := reopenCatalogEngine(t, engine.directory, transport)
	if cursor := restarted.downloader.hongguoCatalogSnapshot().Feeds["category:short_play"]; cursor.Offset != 18 || len(restarted.catalogs["hongguo|short_play"]) != 2 {
		t.Fatal("restart resumed an offset whose data had not been saved")
	}
	if status := restarted.sourceStatus(sourceHongguo); status.Stage != "上次保存未完成" || restarted.nativeCached("hongguo|short_play").Fresh {
		t.Fatal("restart hid the interrupted save", status)
	}
	status, err := engine.startSourceTask(sourceHongguo, "retrySave", nativeDrama{})
	if err != nil || status.StorageError != "" || len(offsets) != 2 {
		t.Fatal("save retry failed or issued network requests", err, status)
	}
	restarted = reopenCatalogEngine(t, engine.directory, transport)
	if len(restarted.catalogs["hongguo|short_play"]) != 3 || restarted.downloader.hongguoCatalogSnapshot().Feeds["category:short_play"].Offset != 36 {
		t.Fatal("retry did not persist pending items with their cursor")
	}
	if result, err := restarted.nativeCatalog(context.Background(), input); err != nil || result.Warning != "" || !reflect.DeepEqual(offsets, []int{0, 18, 36}) {
		t.Fatal("continuation did not resume after the saved page", err, offsets)
	}
}

func TestNativeCatalogSizeLimitPreservesPreviousFile(t *testing.T) {
	var requests atomic.Int32
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("network forbidden")
	})
	if err := engine.saveCatalogCache(sourceHongguo, &nativeCatalogResult{Items: []nativeDrama{{ID: "hongguo:1", Title: "已保存"}}, Page: 1, HasMore: true}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(engine.directory, "catalogs.json")
	before, _ := os.ReadFile(path)
	result := nativeCatalogResult{Items: []nativeDrama{{ID: "hongguo:2", Title: "未保存", Description: strings.Repeat("x", nativeCatalogMaxBytes)}}, Page: 2, HasMore: true}
	err := engine.saveCatalogCache(sourceHongguo, &result)
	if !errors.Is(err, errNativeCatalogLimit) || result.Fresh || !strings.Contains(result.Warning, "上限") {
		t.Fatal("oversized data was silently accepted", err, result.Warning)
	}
	if _, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHongguo, Page: 3}); err != nil || requests.Load() != 0 {
		t.Fatal("size limit did not stop additional paging", err, requests.Load())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(engine.nativeCached(sourceHongguo).Items) != 2 {
		t.Fatal("size limit lost either the previous disk data or pending memory data")
	}
	restarted := reopenCatalogEngine(t, engine.directory, nil)
	if cached := restarted.nativeCached(sourceHongguo); len(cached.Items) != 1 || cached.Page != 1 {
		t.Fatal("restart skipped the last durable catalog")
	}
}

func TestNativeCatalogMetadataSaveExcludesInFlightCursor(t *testing.T) {
	paused, release := make(chan struct{}), make(chan struct{})
	transport := sourceFixtureTransport(func(request *http.Request) (*http.Response, error) {
		payload := nativeHongguoRequestForTest(t, request)
		if payload.Offset == 18 && payload.Scene == "comic_series" {
			close(paused)
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		base := map[string]int{"default": 1000, "comic_series": 2000, "ai_series": 3000}[payload.Scene]
		return nativeHongguoResponse(request, payload.Offset+18, true, "fixture", strconv.Itoa(base+payload.Offset)), nil
	})
	engine := sourceFixtureEngine(t, transport)
	if _, err := engine.nativeCatalog(context.Background(), nativeInput{Source: sourceHongguo, Page: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := engine.nativeCatalog(ctx, nativeInput{Source: sourceHongguo, Page: 2})
		if err == nil && result.Warning != "" {
			err = errors.New(result.Warning)
		}
		done <- err
	}()
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("catalog never reached the in-flight checkpoint")
	}
	engine.saveDetailMetadata(nativeDrama{ID: "hongguo:1000", Source: sourceHongguo, Title: "补齐名称"})
	restarted := reopenCatalogEngine(t, engine.directory, transport)
	if cursor := restarted.downloader.hongguoCatalogSnapshot().Feeds["short_play"]; cursor.Offset != 18 {
		t.Error("an unrelated metadata write persisted an uncommitted feed cursor")
	}
	if len(restarted.catalogs[sourceHongguo]) != 3 {
		t.Error("in-flight catalog data was partially persisted")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	restarted = reopenCatalogEngine(t, engine.directory, transport)
	if restarted.downloader.hongguoCatalogSnapshot().Feeds["short_play"].Offset != 36 || len(restarted.catalogs[sourceHongguo]) != 6 {
		t.Fatal("completed page was not saved with its cursor")
	}
}

func TestNativeSourceRecordSaveFailureAndRetryDuringBackoff(t *testing.T) {
	var requests atomic.Int32
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("network forbidden")
	})
	engine.changeSourceRecord(sourceHongguo, func(record *nativeSourceRecord) {
		record.Stage = "已完成"
		record.Health = &nativeSourceHealth{State: "ok", Sample: "原诊断"}
	})
	path := filepath.Join(engine.directory, "sources.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	engine.changeSourceRecord(sourceHongguo, func(record *nativeSourceRecord) {
		record.RetryAt = time.Now().Add(time.Minute)
		record.Health = &nativeSourceHealth{State: "catalogOnly", Sample: "新诊断"}
	})
	status, err := engine.startSourceTask(sourceHongguo, "retrySave", nativeDrama{})
	if err != nil || status.StorageError == "" || status.Stage != "等待保存" || status.Health.Sample != "新诊断" || requests.Load() != 0 {
		t.Fatal("failed task persistence was hidden or blocked by network cooldown", err, status)
	}
	if err := os.Rename(path, path+".blocked"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	status, err = engine.startSourceTask(sourceHongguo, "retrySave", nativeDrama{})
	if err != nil || status.StorageError != "" || status.Stage != "已完成" || requests.Load() != 0 {
		t.Fatal("task record retry did not recover", err, status)
	}
	restarted := reopenCatalogEngine(t, engine.directory, nil)
	if restarted.sourceStatus(sourceHongguo).Health.Sample != "新诊断" {
		t.Fatal("retry did not save the new health report")
	}
	engine.mu.Lock()
	record := engine.sourceRecords[sourceHongguo]
	record.Stage = strings.Repeat("x", nativeSourceMaxBytes)
	engine.sourceRecords[sourceHongguo] = record
	err = engine.saveSourceRecordsLocked()
	engine.mu.Unlock()
	if !errors.Is(err, errNativeSourceLimit) || engine.sourceStatus(sourceHongguo).StorageError == "" {
		t.Fatal("oversized task records were silently dropped", err)
	}
	restarted = reopenCatalogEngine(t, engine.directory, nil)
	if restarted.sourceStatus(sourceHongguo).Health.Sample != "新诊断" {
		t.Fatal("oversized records replaced the previous saved report")
	}
}
