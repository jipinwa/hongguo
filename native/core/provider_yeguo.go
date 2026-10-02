package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var yeguoFilterFields = []string{"theme", "setting", "background", "time", "recommend"}

func validYeguoCategory(category string) bool {
	field, value, valid := strings.Cut(category, ":")
	if !valid || !webProviderNumericID.MatchString(value) {
		return false
	}
	for _, known := range yeguoFilterFields {
		if field == known {
			return true
		}
	}
	return false
}

func yeguoFlag(value any) (bool, bool) {
	if flag, valid := value.(bool); valid {
		return flag, true
	}
	switch nativeText(value) {
	case "0", "false":
		return false, true
	case "1", "true":
		return true, true
	}
	return false, false
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil && nativeText(value) != "" {
			return value
		}
	}
	return nil
}

func (d *Downloader) fetchYeguoCategories(ctx context.Context) ([]nativeCategory, error) {
	data, err := d.yeguoClient().call(ctx, "/api/home/contentOptions", nil)
	if err != nil {
		return nil, err
	}
	filters, valid := data["video_filter"].(map[string]any)
	if !valid {
		return nil, errors.New("野果分类数据格式无效")
	}
	var categories []nativeCategory
	seen := map[string]bool{}
	for _, field := range yeguoFilterFields {
		filter, _ := filters[field].(map[string]any)
		options, _ := filter["list"].([]any)
		if len(options) > 300 {
			return nil, errors.New("野果分类条目过多")
		}
		for _, option := range options {
			row, _ := option.(map[string]any)
			value, name := mapString(row, "value"), mapString(row, "name")
			if value == "0" {
				continue
			}
			id := field + ":" + value
			if !validYeguoCategory(id) || name == "" || len([]rune(name)) > 48 || seen[id] {
				return nil, errors.New("野果分类数据包含无效或重复条目")
			}
			seen[id] = true
			categories = append(categories, nativeCategory{ID: id, Name: name})
		}
	}
	if len(categories) == 0 {
		return nil, errors.New("野果暂未返回分类")
	}
	return categories, nil
}

func yeguoDramaFromMap(row map[string]any, site string) (Drama, error) {
	id, title := mapString(row, "video_id", "id"), mapString(row, "title")
	if !webProviderNumericID.MatchString(id) || title == "" {
		return Drama{}, errors.New("野果剧集资料不完整")
	}
	episodes := 0
	for _, key := range []string{"episode_count", "episodes", "total_serial"} {
		if count, valid := webProviderInteger(row[key], 100000); valid {
			episodes = max(episodes, count)
		}
	}
	status := ""
	switch mapString(row, "serialize_status") {
	case "1":
		status = "ongoing"
	case "2":
		status = "finished"
	}
	var tags []string
	seenTags := map[string]bool{}
	if values, valid := row["tags"].([]any); valid {
		for _, value := range values {
			if tag, valid := value.(string); valid && len(tags) < 32 {
				tag = strings.TrimSpace(tag)
				if tag != "" && len([]rune(tag)) <= 48 && !seenTags[tag] {
					seenTags[tag] = true
					tags = append(tags, tag)
				}
			}
		}
	}
	cover := providerCoverAddress(row["cover"], site+"/")
	description := truncate(mapString(row, "description"), 12000)
	drama := Drama{ID: providerDramaID(sourceYeguo, id), Source: sourceYeguo, SourceID: id,
		Title: truncate(title, 512), Name: truncate(title, 512), Desc: description, Intro: description,
		Cover: cover, CoverURL: cover, EpisodeCount: episodes, TotalEpisode: episodes,
		CategoryName: "短剧", ChannelName: "野果", Views: normalizeViews(mapString(row, "play_count", "play_count_text")),
		Tags: tags, ReleaseStatus: status, OnlineDate: providerReleaseDate(mapString(row, "published_at", "created_at"))}
	if vip, known := yeguoFlag(row["is_vip"]); known {
		drama.VIP = &vip
	}
	return drama, nil
}

func (d *Downloader) fetchYeguoCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 1000000 || len(query) > 1024 || category != "" && !validYeguoCategory(category) {
		return nil, false, errors.New("野果目录查询参数无效")
	}
	route := "/api/theater/exploreList"
	values := url.Values{"page": {strconv.Itoa(page)}, "limit": {"20"}}
	if query != "" {
		route = "/api/search/result"
		values.Set("keyword", query)
		values.Set("tab", "video")
	} else if category != "" {
		field, value, _ := strings.Cut(category, ":")
		values.Set(field, value)
	}
	data, err := d.yeguoClient().call(ctx, route, values)
	if err != nil {
		return nil, false, err
	}
	rows, valid := data["list"].([]any)
	if !valid || len(rows) > 500 {
		return nil, false, errors.New("野果目录格式无效")
	}
	if actualPage, pageValid := webProviderInteger(firstPresent(data, "page", "page_num", "pageNum", "current_page", "currentPage"), 1000000); pageValid && actualPage != page {
		return nil, false, errors.New("野果分页信息无效，请重试")
	}
	limit, limitValid := webProviderInteger(firstPresent(data, "limit", "page_size", "pageSize", "per_page", "perPage"), 500)
	if !limitValid || limit < 1 || limit < len(rows) {
		limit = len(rows)
	}
	total, totalValid := webProviderInteger(firstPresent(data, "total", "total_count", "totalCount"), 100000000)
	if totalValid && total < len(rows) {
		totalValid = false
	}
	hasMore := limit > 0 && len(rows) >= limit
	if totalValid && limit > 0 {
		hasMore = page < (total+limit-1)/limit
	}
	if data["has_more"] != nil {
		more, valid := yeguoFlag(data["has_more"])
		if !valid {
			return nil, false, errors.New("野果分页信息无效，请重试")
		}
		hasMore = more
	}
	if hasMore && len(rows) == 0 {
		return nil, false, errors.New("野果未返回应有的目录页，请重试")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, value := range rows {
		row, _ := value.(map[string]any)
		drama, err := yeguoDramaFromMap(row, d.yeguoClient().siteURL())
		if err != nil {
			return nil, false, err
		}
		if seen[drama.ID] {
			return nil, false, errors.New("野果目录包含重复剧集")
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items, hasMore, nil
}

func yeguoEpisodePage(site, id string, episode int) string {
	address := site + "/drama/video/" + id + "/"
	if episode > 1 {
		address += "ep-" + strconv.Itoa(episode) + "/"
	}
	return address
}

func (d *Downloader) fetchYeguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("野果剧集 ID 无效")
	}
	row, err := d.yeguoClient().call(ctx, "/api/playlet/detail", url.Values{
		"video_id": {sourceID}, "id": {sourceID}, "episode_id": {"0"}, "related_limit": {"0"},
	})
	if err != nil {
		return Drama{}, nil, err
	}
	site := d.yeguoClient().siteURL()
	drama, err := yeguoDramaFromMap(row, site)
	if err != nil {
		return Drama{}, nil, err
	}
	if drama.SourceID != sourceID {
		return Drama{}, nil, errors.New("野果详情与请求剧集不符")
	}
	episodes, valid := row["episodes"].([]any)
	if !valid || len(episodes) > 10000 {
		return Drama{}, nil, errors.New("野果分集目录格式无效")
	}
	chapters := make([]Chapter, 0, len(episodes))
	seenIDs, seenNumbers := map[string]bool{}, map[int]bool{}
	for _, value := range episodes {
		episode, _ := value.(map[string]any)
		if advertisement, _ := yeguoFlag(episode["is_adv"]); advertisement {
			continue
		}
		id := mapString(episode, "id")
		number, valid := webProviderInteger(episode["sort"], 100000)
		if !webProviderNumericID.MatchString(id) || !valid || number < 1 || seenIDs[id] || seenNumbers[number] {
			return Drama{}, nil, errors.New("野果分集编号无效或重复，请刷新详情")
		}
		seenIDs[id], seenNumbers[number] = true, true
		title := firstNonEmpty(mapString(episode, "title"), fmt.Sprintf("第 %d 集", number))
		chapters = append(chapters, Chapter{ID: providerChapterID(sourceYeguo, sourceID, id), Source: sourceYeguo,
			Title: truncate(title, 256), CurrentEpisode: rawEpisode(number), VideoURL: "yeguo-play://" + id,
			PageURL: yeguoEpisodePage(site, sourceID, number), Referer: site + "/"})
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(j + 1))
		return left < right
	})
	return drama, chapters, nil
}

func yeguoMediaQuality(resolution string) int {
	parts := strings.Split(strings.ToLower(resolution), "x")
	if len(parts) != 2 {
		return 0
	}
	width, widthValid := webProviderInteger(parts[0], 16384)
	height, heightValid := webProviderInteger(parts[1], 16384)
	if !widthValid || !heightValid || width < 1 || height < 1 {
		return 0
	}
	return min(width, height)
}

func (d *Downloader) resolveYeguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceYeguo, sourceID, "")
	if !valid || source != sourceYeguo || !webProviderNumericID.MatchString(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("野果播放分集信息无效，请刷新详情")
	}
	episodeID := strings.TrimPrefix(task.Chapter.ID, prefix)
	if !webProviderNumericID.MatchString(episodeID) {
		return providerMedia{}, errors.New("野果播放分集 ID 无效")
	}
	row, err := d.yeguoClient().call(ctx, "/api/playlet/play", url.Values{
		"playlet_id": {sourceID}, "video_id": {sourceID}, "episode_id": {episodeID},
	})
	if err != nil {
		return providerMedia{}, err
	}
	if mapString(row, "playlet_id") != sourceID || mapString(row, "id") != episodeID {
		return providerMedia{}, errors.New("野果返回的播放地址与请求分集不符")
	}
	if advertisement, _ := yeguoFlag(row["is_adv"]); advertisement {
		return providerMedia{}, errors.New("野果未返回该集正片，请重试")
	}
	number, valid := webProviderInteger(row["episode_sort"], 100000)
	if !valid || number < 1 {
		return providerMedia{}, errors.New("野果播放分集编号无效")
	}
	referer := yeguoEpisodePage(d.yeguoClient().siteURL(), sourceID, number)
	quality := yeguoMediaQuality(mapString(row, "resolution"))
	var options []providerMedia
	seen := map[string]bool{}
	for _, field := range []string{"video_url", "video_url_h265"} {
		raw := mapString(row, field)
		if raw == "" {
			continue
		}
		address := resolveProviderURL(referer, raw)
		if !isProviderHTTPMediaURL(address) || seen[address] {
			continue
		}
		seen[address] = true
		media := providerMedia{URL: address, Referer: referer, Quality: quality}
		if duration, err := strconv.ParseFloat(mapString(row, "episode_duration"), 64); err == nil && duration > 0 && duration <= 604800 {
			media.Duration = time.Duration(duration * float64(time.Second))
		}
		options = append(options, media)
	}
	if len(options) == 0 {
		return providerMedia{}, errors.New("野果未提供该集播放地址，请重试或确认站源权限")
	}
	media := options[0]
	media.Variants = options
	return d.prepareWebProviderMedia(ctx, media, "野果")
}
