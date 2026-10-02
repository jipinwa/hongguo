package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// 青空次元（sorani）原生站源：纯 JSON API，番剧/剧场/特摄三类。
//
// 上游接口（实测 2026-09）：
//   GET /video?page=&size=&categoryId=        目录
//   GET /video/{id}                           详情（含 episodes[]）
//   GET /video/search?keyword=&page=&size=    搜索
//   GET /video/tags                           标签（未用于目录，保留扩展位）
//   GET /video/episode/{episodeId}/play?lineCode=anime_jp_m3u8   播放
//
// 默认线路 anime_jp_m3u8 为实测有效线；lineCode 必须精确匹配，其余取值返回空 playUrl。

const (
	soraniLineCodeDefault = "anime_jp_m3u8"

	soraniMaxPage     = 100000
	soraniMaxCategory = 128
	soraniMaxKeyword  = 256
	soraniMaxID       = 64
)

// soraniCategories 是上游固定的三个一级分类，实测均有数据：
// categoryId=1 → TV番剧 3782；2 → 剧场动画 848；5 → 特摄剧场 65。
var soraniCategories = []nativeCategory{
	{ID: "1", Name: "TV番剧"},
	{ID: "2", Name: "剧场动画"},
	{ID: "5", Name: "特摄剧场"},
}

func validSoraniCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > soraniMaxCategory || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	for _, entry := range soraniCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

// soraniNumericID 校验上游自增数字 ID（videoId / episodeId）。
func soraniNumericID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > soraniMaxID {
		return false
	}
	number, err := strconv.Atoi(value)
	return err == nil && number > 0
}

func soraniCover(value any) string {
	return providerCoverAddress(value, soraniSiteBaseURL+"/")
}

// soraniDramaFromMap 把上游记录映射成 Drama。
// 记录字段跨列表/详情/搜索三种响应一致（实测），因此复用同一映射。
func soraniDramaFromMap(row map[string]any) (Drama, error) {
	if row == nil {
		return Drama{}, errors.New("青空次元返回的剧集信息不完整")
	}
	id := mapString(row, "id", "videoId")
	title := mapString(row, "title", "name", "videoTitle")
	if !soraniNumericID(id) || strings.TrimSpace(title) == "" {
		return Drama{}, errors.New("青空次元返回的剧集信息不完整")
	}
	description := mapString(row, "summary", "description", "intro", "desc")
	cover := soraniCover(firstAnyValue(row, "cover", "coverLarge", "coverSmall", "coverThumb", "backgroundImage"))

	episodes := 0
	for _, key := range []string{"episodeCount", "totalEpisode", "episodes", "total"} {
		if value, ok := row[key]; ok {
			if count, err := strconv.Atoi(nativeText(value)); err == nil && count > episodes && count <= 100000 {
				episodes = count
			}
		}
	}

	category := mapString(row, "categoryName", "category")
	tags := make([]string, 0, 8)
	seen := map[string]bool{}
	addTag := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len([]rune(value)) > 48 || len(tags) >= 16 {
			return
		}
		seen[value] = true
		tags = append(tags, value)
	}
	if name := mapString(row, "categoryName"); name != "" && name != category {
		addTag(name)
	}
	for _, value := range mapStringSlice(row, "tags", "tagList", "categoryTags") {
		addTag(value)
	}
	if alias := mapString(row, "alias"); alias != "" {
		addTag(truncate(alias, 48))
	}
	if area := mapString(row, "area"); area != "" {
		addTag(truncate(area, 48))
	}
	if year := mapString(row, "year", "publishDate"); year != "" {
		if digits := leadingYear(year); digits != "" {
			addTag(digits + "年")
		}
	}
	if actor := mapString(row, "actors", "actor"); actor != "" {
		addTag("声优 " + truncate(actor, 32))
	}
	score := mapString(row, "bangumiRating", "score")
	if value, err := strconv.ParseFloat(score, 64); err != nil || value <= 0 || value > 10 {
		score = ""
	} else if value > 0 {
		addTag("评分 " + score)
	}
	if score != "" {
		score = truncate(score, 16)
	}

	if category == "" {
		category = "番剧"
	}
	return Drama{
		ID: providerDramaID(sourceSorani, id), Source: sourceSorani, SourceID: id,
		Title: truncate(title, 256), Name: truncate(title, 256),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover,
		TotalEpisode: episodes, EpisodeCount: episodes,
		Category: category, ChannelName: "青空次元",
		Tags: tags, Score: score,
	}, nil
}

func leadingYear(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return ""
	}
	digits := value[:4]
	for _, r := range digits {
		if r < '0' || r > '9' {
			return ""
		}
	}
	year, err := strconv.Atoi(digits)
	if err != nil || year < 1900 || year > 2200 {
		return ""
	}
	return digits
}

func firstAnyValue(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok && nativeText(value) != "" {
			return value
		}
	}
	return nil
}

// soraniPayload 兼容 {code,data:{...}} 与 {code,data:[...]} 两种包裹。
func soraniPayload(value any) any {
	row, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if data, exists := row["data"]; exists {
		return data
	}
	return value
}

// soraniRecordList 从 data 中取出记录数组，兼容 records / list / 裸数组。
func soraniRecordList(payload any) ([]any, bool) {
	switch value := payload.(type) {
	case []any:
		return value, true
	case map[string]any:
		for _, key := range []string{"records", "list", "items", "rows"} {
			if rows, ok := value[key].([]any); ok {
				return rows, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func (d *Downloader) soraniRequest(ctx context.Context, route string, query url.Values, limit int64) (any, error) {
	return d.soraniClient().get(ctx, route, query, limit)
}

// fetchSoraniCategories 返回固定三分类，无需远端探测。
func (d *Downloader) fetchSoraniCategories() []nativeCategory {
	return append([]nativeCategory(nil), soraniCategories...)
}

func (d *Downloader) fetchSoraniCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > soraniMaxPage {
		return nil, false, errors.New("青空次元目录页码无效")
	}
	if !validSoraniCategory(category) {
		return nil, false, errors.New("青空次元内容分类无效")
	}
	if len(query) > soraniMaxKeyword {
		return nil, false, errors.New("青空次元搜索关键词过长")
	}
	const pageSize = 24
	values := url.Values{"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(pageSize)}}
	route := "/video"
	switch {
	case query != "":
		route = "/video/search"
		values.Set("keyword", query)
	case category != "":
		values.Set("categoryId", category)
	}
	value, err := d.soraniRequest(ctx, route, values, 4<<20)
	if err != nil {
		return nil, false, err
	}
	payload := soraniPayload(value)
	rows, ok := soraniRecordList(payload)
	if !ok || len(rows) > 500 {
		return nil, false, errors.New("青空次元目录数据格式无效")
	}
	hasMore := len(rows) >= pageSize
	if meta, ok := payload.(map[string]any); ok {
		if total, err := strconv.Atoi(nativeText(meta["total"])); err == nil && total >= 0 && total <= 100000000 {
			hasMore = page*pageSize < total
		}
	}
	if hasMore && len(rows) == 0 {
		return nil, false, errors.New("青空次元未返回应有的目录页，请重试")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, entry := range rows {
		row, _ := entry.(map[string]any)
		drama, err := soraniDramaFromMap(row)
		if err != nil {
			return nil, false, err
		}
		if seen[drama.ID] {
			return nil, false, errors.New("青空次元目录包含重复剧集，请重试")
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items, hasMore, nil
}

func (d *Downloader) fetchSoraniDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !soraniNumericID(sourceID) {
		return Drama{}, nil, errors.New("青空次元剧集 ID 无效")
	}
	value, err := d.soraniRequest(ctx, "/video/"+url.PathEscape(sourceID), nil, 8<<20)
	if err != nil {
		return Drama{}, nil, err
	}
	row, _ := soraniPayload(value).(map[string]any)
	if row == nil {
		return Drama{}, nil, errors.New("青空次元详情数据格式无效")
	}
	drama, err := soraniDramaFromMap(row)
	if err != nil {
		return Drama{}, nil, err
	}
	if drama.SourceID != sourceID {
		return Drama{}, nil, errors.New("青空次元详情与请求剧集不符")
	}
	rows, ok := row["episodes"].([]any)
	if !ok || len(rows) > 10000 {
		return Drama{}, nil, errors.New("青空次元分集目录格式无效")
	}
	chapters := make([]Chapter, 0, len(rows))
	seenIDs, seenOrders := map[string]bool{}, map[int]bool{}
	for index, entry := range rows {
		episode, _ := entry.(map[string]any)
		if episode == nil {
			continue
		}
		if enabled, ok := episode["enabled"].(bool); ok && !enabled {
			continue
		}
		episodeID := mapString(episode, "episodeId", "id")
		if !soraniNumericID(episodeID) || seenIDs[episodeID] {
			return Drama{}, nil, errors.New("青空次元分集目录包含无效或重复分集")
		}
		order := index + 1
		if episode["episodeOrder"] != nil {
			if parsed, err := strconv.Atoi(nativeText(episode["episodeOrder"])); err == nil && parsed > 0 && parsed <= 100000 {
				order = parsed
			}
		}
		if seenOrders[order] {
			return Drama{}, nil, errors.New("青空次元分集编号重复，请刷新目录")
		}
		seenIDs[episodeID], seenOrders[order] = true, true
		title := mapString(episode, "episodeLabel", "title", "displayTitle")
		if title == "" {
			title = fmt.Sprintf("第 %d 集", order)
		}
		// VideoURL 是占位符：真实播放地址在 playerContent 阶段按 episodeId 实时换取，
		// 避免详情页展开时就向上游逐个请求 play 接口。
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceSorani, sourceID, episodeID), Source: sourceSorani,
			Title: truncate(title, 128), CurrentEpisode: json.RawMessage(strconv.Itoa(order)),
			VideoURL: "sorani-play://" + url.PathEscape(episodeID),
			Referer:  soraniSiteBaseURL + "/",
		})
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(j + 1))
		if left == right {
			return chapters[i].ID < chapters[j].ID
		}
		return left < right
	})
	return drama, chapters, nil
}

// resolveSoraniMedia 用章节 ID 里携带的 episodeId 实时换取 m3u8 直链。
func (d *Downloader) resolveSoraniMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceSorani, sourceID, "")
	if !valid || source != sourceSorani || !soraniNumericID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("青空次元播放分集信息无效，请刷新详情")
	}
	episodeID := strings.TrimPrefix(task.Chapter.ID, prefix)
	if !soraniNumericID(episodeID) {
		return providerMedia{}, errors.New("青空次元分集 ID 无效")
	}
	// 目前仅 anime_jp_m3u8 一条线实测有效；lineCode 已编码进章节 ID 的前缀之外无额外承载位，
	// 因此固定走默认线。若上游将来提供多线，可把 lineCode 追加进 providerChapterID 的 chapterKey。
	lineCode := soraniLineCodeDefault
	value, err := d.soraniRequest(ctx, "/video/episode/"+url.PathEscape(episodeID)+"/play",
		url.Values{"lineCode": {lineCode}}, 64<<10)
	if err != nil {
		return providerMedia{}, err
	}
	row, _ := soraniPayload(value).(map[string]any)
	address := mapString(row, "playUrl", "url")
	if address == "" {
		return providerMedia{}, fmt.Errorf("青空次元该分集暂无可用播放地址（线路 %s）", lineCode)
	}
	media := providerMedia{URL: address, Referer: soraniSiteBaseURL + "/"}
	parsed, err := url.Parse(address)
	if err != nil || !isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("青空次元播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		playlist, finalURL, err := d.fetchMediaPlaylist(ctx, media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("获取青空次元播放列表失败：%w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
			return providerMedia{}, errors.New("青空次元未返回有效 M3U8，链接可能已失效")
		}
		media.Playlist, media.URL = playlist, finalURL
		media.Duration = m3u8Duration(playlist)
	}
	return media, nil
}
