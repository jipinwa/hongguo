package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type duanjuTokenEntry struct {
	token     string
	expiresAt time.Time
}

var duanjuTokens struct {
	sync.Mutex
	entries map[string]duanjuTokenEntry
}

func (d *Downloader) duanjuCachedToken(source string, ttl time.Duration, fetch func(context.Context) (string, error)) (string, error) {
	duanjuTokens.Lock()
	entry, found := duanjuTokens.entries[source]
	duanjuTokens.Unlock()
	if found && entry.token != "" && time.Now().Before(entry.expiresAt) {
		return entry.token, nil
	}
	token, err := fetch(context.Background())
	if err != nil {
		return "", err
	}
	duanjuTokens.Lock()
	if duanjuTokens.entries == nil {
		duanjuTokens.entries = map[string]duanjuTokenEntry{}
	}
	duanjuTokens.entries[source] = duanjuTokenEntry{token: token, expiresAt: time.Now().Add(ttl)}
	duanjuTokens.Unlock()
	return token, nil
}

func (d *Downloader) duanjuYaguoLoginURL() string {
	if host := d.providerHosts[sourceYaguo]; host != "" && strings.TrimRight(host, "/") != yaguoBaseURL {
		return strings.TrimRight(host, "/") + "/user/v1/account/login"
	}
	return yaguoLoginURL
}

func (d *Downloader) duanjuYaguoToken(ctx context.Context) (string, error) {
	return d.duanjuCachedToken(sourceYaguo, 6*time.Hour, func(_ context.Context) (string, error) {
		payload, err := json.Marshal(map[string]any{"device": "24250683a3bdb3f118dff25ba4b1cba1a"})
		if err != nil {
			return "", err
		}
		response, err := d.duanjuJSON(context.Background(), duanjuRequest{
			Source:  sourceYaguo,
			Method:  http.MethodPost,
			Address: d.duanjuYaguoLoginURL(),
			Headers: map[string]string{"platform": "1", "Content-Type": "application/json"},
			Body:    payload,
			Timeout: 15 * time.Second,
		})
		if err != nil {
			return "", fmt.Errorf("芽果登录失败：%w", err)
		}
		token := duanjuFindString(duanjuFindMap(response, "data"), "token")
		if token == "" {
			token = duanjuFindString(response, "token")
		}
		if token == "" {
			return "", errors.New("芽果未返回有效的访问令牌")
		}
		return token, nil
	})
}

func (d *Downloader) duanjuYaguoRequest(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	token, err := d.duanjuYaguoToken(ctx)
	if err != nil {
		return nil, err
	}
	address := d.duanjuBaseURL(sourceYaguo) + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	return d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceYaguo,
		Address: address,
		Referer: d.duanjuBaseURL(sourceYaguo) + "/",
		Headers: map[string]string{
			"authorization": token,
			"platform":      "1",
			"version_name":  "3.8.3.1",
		},
	})
}

func yaguoDramaFromNode(node map[string]any, base string) Drama {
	theater := duanjuFindMap(node, "theater")
	if theater == nil {
		theater = node
	}
	id := duanjuFindString(theater, "id", "theater_id", "theater_parent_id")
	if id == "" {
		return Drama{}
	}
	total := duanjuFindInt(theater, "total", "total_num", "current_num")
	return Drama{
		ID:           providerDramaID(sourceYaguo, id),
		Source:       sourceYaguo,
		SourceID:     id,
		Title:        duanjuFindString(theater, "title", "name"),
		Intro:        duanjuFindString(theater, "descrip", "introduction", "desc"),
		Cover:        duanjuCover(base, theater, "cover_url", "son_cover_url", "cover"),
		EpisodeCount: json.Number(strconv.Itoa(total)),
		Category:     duanjuCSV(duanjuStringList(theater["tags"])),
		ChannelName:  duanjuSourceName(sourceYaguo),
	}
}

func (d *Downloader) fetchYaguoCategories(ctx context.Context) ([]nativeCategory, error) {
	response, err := d.duanjuYaguoRequest(ctx, "/cloud/v2/theater/classes", nil)
	if err != nil {
		return nil, err
	}
	var categories []nativeCategory
	seen := map[string]bool{}
	appendClass := func(entries []any) {
		for _, entry := range entries {
			node, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			id := duanjuFindString(node, "id")
			name := duanjuFindString(node, "class_name", "name")
			if id == "" || name == "" || seen[id] || len([]rune(name)) > 24 {
				continue
			}
			seen[id] = true
			categories = append(categories, nativeCategory{ID: id, Name: name})
		}
	}
	for _, group := range duanjuFindList(response, "data") {
		mapped, ok := group.(map[string]any)
		if !ok {
			continue
		}
		appendClass(duanjuFindList(mapped, "alg_class"))
	}
	if len(categories) == 0 {
		return nil, errors.New("芽果未返回有效分类")
	}
	return categories, nil
}

func (d *Downloader) fetchYaguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	base := d.duanjuBaseURL(sourceYaguo)
	classID := strings.TrimSpace(category)
	if classID == "9" {
		response, err := d.duanjuYaguoRequest(ctx, "/cloud/v1/first_level_ranking/detail", url.Values{"id": {"1"}})
		if err != nil {
			return nil, false, err
		}
		var items []Drama
		for _, entry := range duanjuFindList(duanjuFindMap(response, "data"), "list") {
			node, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if drama := yaguoDramaFromNode(node, base); drama.ID != "" {
				items = append(items, drama)
			}
		}
		return items, false, nil
	}
	if classID != "" && !webProviderNumericID.MatchString(classID) {
		classID = ""
	}
	response, err := d.duanjuYaguoRequest(ctx, "/cloud/v2/theater/home_page", url.Values{
		"theater_class_id": {classID},
		"type":             {"1"},
		"class2_ids":       {"0"},
		"page_num":         {strconv.Itoa(page)},
		"page_size":        {"24"},
	})
	if err != nil {
		return nil, false, err
	}
	data := duanjuFindMap(response, "data")
	var items []Drama
	for _, entry := range duanjuFindList(data, "list") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := yaguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	total := duanjuFindInt(data, "total")
	hasMore := !duanjuFindBool(data, "is_end") && len(items) > 0 && total > page*len(items)
	return items, hasMore, nil
}

func duanjuFindBool(node map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, found := node[key]; found {
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				return typed == "true" || typed == "1"
			case float64:
				return typed != 0
			}
		}
	}
	return false
}

func (d *Downloader) fetchYaguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceYaguo)
	response, err := d.duanjuYaguoRequest(ctx, "/v2/theater_parent/detail", url.Values{"theater_parent_id": {sourceID}})
	if err != nil {
		return Drama{}, nil, err
	}
	data := duanjuFindMap(response, "data")
	if data == nil {
		return Drama{}, nil, errors.New("芽果未返回剧集资料")
	}
	drama := Drama{
		ID:           providerDramaID(sourceYaguo, sourceID),
		Source:       sourceYaguo,
		SourceID:     sourceID,
		Title:        duanjuFindString(data, "title", "share_title"),
		Intro:        duanjuFindString(data, "descrip", "introduction", "share_title"),
		Cover:        duanjuCover(base, data, "cover_url", "share_cover"),
		EpisodeCount: json.Number(strconv.Itoa(duanjuFindInt(data, "total"))),
		ReleaseStatus: duanjuReleaseStatus(
			strconv.Itoa(duanjuFindInt(data, "is_over")),
			duanjuFindInt(data, "total"), duanjuFindInt(data, "current_num"),
		),
		ChannelName: duanjuSourceName(sourceYaguo),
	}
	if drama.ReleaseStatus == "" && duanjuFindInt(data, "is_over") == 2 {
		drama.ReleaseStatus = "finished"
	}
	var chapters []Chapter
	for index, entry := range duanjuFindList(data, "theaters") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoURL := duanjuFindString(node, "son_video_url", "video_url")
		if videoURL == "" {
			continue
		}
		number := duanjuFindInt(node, "num", "sort")
		if number <= 0 {
			number = index + 1
		}
		title := duanjuFindString(node, "son_title", "title")
		chapters = append(chapters, duanjuChapter(sourceYaguo, sourceID, number, title, videoURL, "", base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("芽果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	drama.EpisodeCount = json.Number(strconv.Itoa(len(chapters)))
	return drama, chapters, nil
}

func (d *Downloader) searchYaguo(ctx context.Context, query string) ([]Drama, error) {
	token, err := d.duanjuYaguoToken(ctx)
	if err != nil {
		return nil, err
	}
	base := d.duanjuBaseURL(sourceYaguo)
	payload, err := json.Marshal(map[string]any{"text": query})
	if err != nil {
		return nil, err
	}
	response, err := d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceYaguo,
		Method:  http.MethodPost,
		Address: base + "/v3/search",
		Referer: base + "/",
		Headers: map[string]string{"authorization": token, "platform": "1", "version_name": "3.8.3.1", "Content-Type": "application/json"},
		Body:    payload,
	})
	if err != nil {
		return nil, err
	}
	rows := duanjuFindList(duanjuFindMap(response, "data"), "list")
	if rows == nil {
		rows = duanjuFindList(duanjuFindMap(duanjuFindMap(response, "data"), "theater"), "search_data")
	}
	var items []Drama
	for _, entry := range rows {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := yaguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, nil
}

func sortDuanjuChapters(chapters []Chapter) {
	sort.SliceStable(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(j + 1))
		return left < right
	})
}
