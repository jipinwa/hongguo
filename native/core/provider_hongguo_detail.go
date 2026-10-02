package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type hongguoDetailEntry struct {
	Drama     Drama
	Chapters  []Chapter
	ExpiresAt time.Time
}

type hongguoDetailCall struct {
	done  chan struct{}
	entry hongguoDetailEntry
	err   error
}

func (downloader *Downloader) hongguoAppDetail(ctx context.Context, seriesID string) (hongguoDetailEntry, error) {
	if !hongguoNumericID.MatchString(seriesID) {
		return hongguoDetailEntry{}, errors.New("红果剧集 ID 无效")
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	if cached, found := client.details[seriesID]; found && time.Now().Before(cached.ExpiresAt) {
		client.mu.Unlock()
		return cached, nil
	}
	if pending := client.pending[seriesID]; pending != nil {
		client.mu.Unlock()
		select {
		case <-ctx.Done():
			return hongguoDetailEntry{}, ctx.Err()
		case <-pending.done:
			if (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return downloader.hongguoAppDetail(ctx, seriesID)
			}
			return pending.entry, pending.err
		}
	}
	pending := &hongguoDetailCall{done: make(chan struct{})}
	client.pending[seriesID] = pending
	client.mu.Unlock()
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": seriesID})
	var entry hongguoDetailEntry
	if err == nil {
		entry, err = parseHongguoAppDetail(result, seriesID)
	}
	client.mu.Lock()
	if err == nil {
		if len(client.details) >= 128 {
			client.details = map[string]hongguoDetailEntry{}
		}
		entry.ExpiresAt = time.Now().Add(5 * time.Minute)
		client.details[seriesID] = entry
	}
	pending.entry, pending.err = entry, err
	delete(client.pending, seriesID)
	close(pending.done)
	client.mu.Unlock()
	return entry, err
}

func parseHongguoAppDetail(result map[string]any, seriesID string) (hongguoDetailEntry, error) {
	detail := nestedMap(result, "data", "video_data")
	returnedID := mapString(detail, "series_id_str", "series_id")
	if returnedID != seriesID {
		return hongguoDetailEntry{}, errors.New("红果 App 未返回所请求的剧集")
	}
	entry := hongguoDetailEntry{Drama: hongguoDramaFromAny(detail, "短剧")}
	seen := map[string]bool{}
	episodes := map[int]bool{}
	for _, row := range anyList(detail["video_list"]) {
		video, _ := row.(map[string]any)
		videoID := mapString(video, "vid")
		index, err := strconv.Atoi(mapString(video, "vid_index"))
		if err != nil || index < 1 || !hongguoNumericID.MatchString(videoID) {
			return entry, errors.New("红果 App 分集编号或视频 ID 无效")
		}
		if identifier := mapString(video, "series_id"); identifier != "" && identifier != seriesID {
			return entry, errors.New("红果 App 返回了其他剧集的分集")
		}
		if seen[videoID] || episodes[index] {
			return entry, errors.New("红果 App 返回了重复分集")
		}
		seen[videoID], episodes[index] = true, true
		entry.Chapters = append(entry.Chapters, Chapter{
			ID: providerChapterID(sourceHongguo, seriesID, videoID), Source: sourceHongguo,
			Title: fmt.Sprintf("第%d集", index), VideoURL: "hongguo-cenc://" + videoID, CurrentEpisode: rawEpisode(index),
		})
	}
	sort.Slice(entry.Chapters, func(left, right int) bool {
		leftIndex, _ := strconv.Atoi(entry.Chapters[left].EpisodeString(left + 1))
		rightIndex, _ := strconv.Atoi(entry.Chapters[right].EpisodeString(right + 1))
		return leftIndex < rightIndex
	})
	total, _ := strconv.Atoi(mapString(detail, "episode_cnt"))
	if len(entry.Chapters) == 0 || total > len(entry.Chapters) {
		return entry, errors.New("红果 App 未返回完整分集，已尝试其他详情入口")
	}
	for index, chapter := range entry.Chapters {
		if chapter.EpisodeString(index+1) != strconv.Itoa(index+1) {
			return entry, errors.New("红果 App 分集列表不连续")
		}
	}
	return entry, nil
}

func (downloader *Downloader) hongguoCachedDrama(drama Drama) Drama {
	source, seriesID, valid := splitProviderDramaID(drama.ID)
	if !valid || source != sourceHongguo {
		return drama
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	if cached, found := client.details[seriesID]; found && time.Now().Before(cached.ExpiresAt) {
		return mergeDramaMetadata(cached.Drama, drama)
	}
	return drama
}
