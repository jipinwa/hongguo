package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const huangjuNewestCategory = "@new"

func validHuangjuID(value string) bool {
	return value != "" && len(value) <= 450 && strings.TrimSpace(value) == value && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\:?#%|\"<>") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func huangjuInteger(value any, maximum int) (int, bool) {
	text := nativeText(value)
	number, err := strconv.Atoi(text)
	return number, err == nil && number >= 0 && number <= maximum
}

func huangjuCategoryNames(row map[string]any) []string {
	var names []string
	seen := map[string]bool{}
	add := func(value any) {
		name := ""
		switch value := value.(type) {
		case map[string]any:
			name, _ = value["name"].(string)
		case string:
			name = value
		}
		name = strings.TrimSpace(name)
		if name != "" && len([]rune(name)) <= 48 && !seen[name] && len(names) < 24 {
			seen[name] = true
			names = append(names, name)
		}
	}
	add(row["category"])
	if entries, ok := row["categories"].([]any); ok {
		for _, entry := range entries {
			add(entry)
		}
	}
	return names
}

func huangjuDramaFromMap(row map[string]any, site, sourceID string) (Drama, error) {
	id := mapString(row, "id")
	slug := mapString(row, "slug")
	title, _ := row["title"].(string)
	if !validHuangjuID(id) || strings.TrimSpace(title) == "" {
		return Drama{}, errors.New("剧果返回的剧集信息不完整")
	}
	if sourceID == "" {
		if !validHuangjuID(slug) {
			return Drama{}, errors.New("剧果返回的剧集地址无效")
		}
		sourceID = slug + "-" + id
	}
	if !validHuangjuID(sourceID) || !strings.HasSuffix(sourceID, "-"+id) {
		return Drama{}, errors.New("剧果详情与请求剧集不符")
	}
	episodes := 0
	if row["totalEpisodes"] != nil {
		var valid bool
		episodes, valid = huangjuInteger(row["totalEpisodes"], 100000)
		if !valid {
			return Drama{}, errors.New("剧果集数格式无效")
		}
	}
	categories := huangjuCategoryNames(row)
	category := "短剧"
	if len(categories) > 0 {
		category = categories[0]
	}
	tags := append([]string{}, categories...)
	if region := mapString(row, "region"); region != "" {
		tags = append(tags, truncate(region, 48))
	}
	if year, valid := huangjuInteger(row["year"], 9999); valid && year >= 1000 {
		tags = append(tags, strconv.Itoa(year)+"年")
	}
	score := mapString(row, "score")
	if value, err := strconv.ParseFloat(score, 64); err == nil && value > 0 && value <= 10 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		tags = append(tags, "评分 "+score)
	} else {
		score = ""
	}
	status := mapString(row, "status")
	if status == "completed" {
		status = "finished"
	}
	if status != "finished" && status != "ongoing" {
		status = ""
	}
	description, _ := row["description"].(string)
	cover := providerCoverAddress(row["coverUrl"], site+"/")
	return Drama{
		ID: providerDramaID(sourceHuangju, sourceID), Source: sourceHuangju, SourceID: sourceID,
		Title: truncate(title, 256), Name: truncate(title, 256), Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: cover, CoverURL: cover, TotalEpisode: episodes, EpisodeCount: episodes,
		Category: category, ChannelName: "剧果", Tags: tags, ReleaseStatus: status, Score: score,
	}, nil
}

func (d *Downloader) fetchHuangjuCategories(ctx context.Context) ([]nativeCategory, error) {
	response, err := d.huangjuClient().get(ctx, "/categories", nil, 256<<10)
	if err != nil {
		return nil, err
	}
	rows, valid := huangjuPayload(response.value).([]any)
	if !valid || len(rows) > 300 {
		return nil, errors.New("剧果分类数据格式无效")
	}
	categories := []nativeCategory{{ID: huangjuNewestCategory, Name: "最新"}}
	seen := map[string]bool{huangjuNewestCategory: true}
	for _, entry := range rows {
		row, _ := entry.(map[string]any)
		id, name := mapString(row, "slug"), mapString(row, "name")
		if id == "hot" {
			continue
		}
		if !validNativeCategory(sourceHuangju, id) || id == "" || name == "" || len([]rune(name)) > 48 || seen[id] {
			return nil, errors.New("剧果分类数据包含无效或重复条目")
		}
		seen[id] = true
		categories = append(categories, nativeCategory{ID: id, Name: name})
	}
	return categories, nil
}

func (d *Downloader) fetchHuangjuCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 1000000 || len(query) > 1024 || !validNativeCategory(sourceHuangju, category) {
		return nil, false, errors.New("剧果目录查询参数无效")
	}
	values := url.Values{"page": {strconv.Itoa(page)}}
	switch {
	case query != "":
		values.Set("q", query)
	case category == huangjuNewestCategory:
		values.Set("sort", "new")
	case category == "":
		values.Set("sort", "hot")
	default:
		values.Set("category", category)
	}
	response, err := d.huangjuClient().get(ctx, "/dramas", values, 4<<20)
	if err != nil {
		return nil, false, err
	}
	row, _ := huangjuPayload(response.value).(map[string]any)
	rows, valid := row["items"].([]any)
	if !valid || len(rows) > 500 {
		return nil, false, errors.New("剧果目录数据格式无效")
	}
	pageSize := 20
	if row["pageSize"] != nil {
		pageSize, valid = huangjuInteger(row["pageSize"], 500)
		if !valid || pageSize < 1 {
			return nil, false, errors.New("剧果分页数据格式无效")
		}
	}
	if row["page"] != nil {
		actualPage, valid := huangjuInteger(row["page"], 1000000)
		if !valid || actualPage != page {
			return nil, false, errors.New("剧果返回的页码与请求不符")
		}
	}
	if len(rows) > pageSize {
		return nil, false, errors.New("剧果分页数据格式无效")
	}
	hasMore := len(rows) >= pageSize
	if row["total"] != nil {
		total, valid := huangjuInteger(row["total"], 100000000)
		if !valid || total < len(rows) {
			return nil, false, errors.New("剧果分页总数无效")
		}
		hasMore = page < (total+pageSize-1)/pageSize
	}
	if hasMore && len(rows) == 0 {
		return nil, false, errors.New("剧果未返回应有的目录页，请重试")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, entry := range rows {
		value, _ := entry.(map[string]any)
		drama, err := huangjuDramaFromMap(value, d.providerBaseURL(sourceHuangju), "")
		if err != nil {
			return nil, false, err
		}
		if seen[drama.ID] {
			return nil, false, errors.New("剧果目录包含重复剧集，请重试")
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items, hasMore, nil
}

func (d *Downloader) fetchHuangjuDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !validHuangjuID(sourceID) {
		return Drama{}, nil, errors.New("剧果剧集地址无效")
	}
	response, err := d.huangjuClient().get(ctx, "/dramas/"+url.PathEscape(sourceID), nil, 8<<20)
	if err != nil {
		return Drama{}, nil, err
	}
	row, _ := huangjuPayload(response.value).(map[string]any)
	drama, err := huangjuDramaFromMap(row, d.providerBaseURL(sourceHuangju), sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	rows, valid := row["episodes"].([]any)
	if !valid || len(rows) > 10000 {
		return Drama{}, nil, errors.New("剧果分集目录格式无效")
	}
	chapters := make([]Chapter, 0, len(rows))
	seenIDs, seenNumbers := map[string]bool{}, map[int]bool{}
	for index, entry := range rows {
		episode, _ := entry.(map[string]any)
		if playable, ok := episode["playable"].(bool); ok && !playable {
			continue
		}
		id := mapString(episode, "id")
		if !validHuangjuID(id) || seenIDs[id] {
			return Drama{}, nil, errors.New("剧果分集目录包含无效或重复分集")
		}
		number := index + 1
		if episode["epNo"] != nil {
			var valid bool
			number, valid = huangjuInteger(episode["epNo"], 100000)
			if !valid {
				return Drama{}, nil, errors.New("剧果分集编号无效")
			}
			if number == 0 {
				number = index + 1
			}
		}
		if seenNumbers[number] {
			return Drama{}, nil, errors.New("剧果分集编号重复，请刷新目录")
		}
		seenIDs[id], seenNumbers[number] = true, true
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceHuangju, sourceID, id), Source: sourceHuangju,
			Title: fmt.Sprintf("第 %d 集", number), CurrentEpisode: json.RawMessage(strconv.Itoa(number)),
			VideoURL: "huangju-play://" + url.PathEscape(id), Referer: d.providerBaseURL(sourceHuangju) + "/",
		})
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(j + 1))
		return left < right
	})
	return drama, chapters, nil
}

var huangjuSignedCookie = regexp.MustCompile(`(?:^|[,;\s])CloudFront-(Policy|Signature|Key-Pair-Id)\s*=\s*([^;,\s"']+)`)

func huangjuExpiration(value any) time.Time {
	text := nativeText(value)
	if stamp, err := strconv.ParseInt(text, 10, 64); err == nil && stamp > 0 && stamp <= 253402300799000 {
		if stamp > 100000000000 {
			return time.UnixMilli(stamp)
		}
		return time.Unix(stamp, 0)
	}
	parsed, _ := time.Parse(time.RFC3339Nano, text)
	return parsed
}

func huangjuMediaCredentials(headers http.Header, address, site string, expiration any) (*providerMediaCredentials, error) {
	cookies := map[string]string{}
	expires := huangjuExpiration(expiration)
	rememberExpiry := func(value time.Time) {
		if !value.IsZero() && (expires.IsZero() || value.Before(expires)) {
			expires = value
		}
	}
	response := &http.Response{Header: headers}
	for _, cookie := range response.Cookies() {
		switch cookie.Name {
		case "CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id":
			cookies[cookie.Name] = cookie.Value
			rememberExpiry(cookie.Expires)
			if cookie.MaxAge != 0 {
				rememberExpiry(time.Now().Add(time.Duration(min(cookie.MaxAge, 86400*365)) * time.Second))
			}
		}
	}
	for _, header := range headers.Values("Set-Cookie") {
		for _, match := range huangjuSignedCookie.FindAllStringSubmatch(header, 12) {
			cookies["CloudFront-"+match[1]] = match[2]
		}
	}
	var values []string
	for _, name := range []string{"CloudFront-Policy", "CloudFront-Signature", "CloudFront-Key-Pair-Id"} {
		value := cookies[name]
		if value == "" || len(value) > 8192 || strings.ContainsAny(value, ",;\"\\") || strings.IndexFunc(value, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
			return nil, errors.New("剧果未返回完整的媒体凭证，请重试")
		}
		values = append(values, name+"="+value)
	}
	if !expires.IsZero() && !time.Now().Before(expires) {
		return nil, errors.New("剧果媒体凭证已过期，请检查设备时间后重试")
	}
	parsed, err := url.Parse(address)
	if err != nil || !isProviderHTTPMediaURL(address) || parsed.User != nil {
		return nil, errors.New("剧果播放地址无效")
	}
	return &providerMediaCredentials{
		cookie: strings.Join(values, "; "), origin: providerMediaOrigin(parsed),
		referer: site + "/", userAgent: huangjuUserAgent, expires: expires,
	}, nil
}

func (d *Downloader) resolveHuangjuMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceHuangju, sourceID, "")
	if !valid || source != sourceHuangju || !validHuangjuID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("剧果播放分集信息无效，请刷新详情")
	}
	id := strings.TrimPrefix(task.Chapter.ID, prefix)
	if !validHuangjuID(id) {
		return providerMedia{}, errors.New("剧果分集地址无效")
	}
	response, err := d.huangjuClient().get(ctx, "/play/"+url.PathEscape(id), nil, 64<<10)
	if err != nil {
		return providerMedia{}, err
	}
	row, _ := huangjuPayload(response.value).(map[string]any)
	address, _ := row["url"].(string)
	credentials, err := huangjuMediaCredentials(response.headers, address, d.providerBaseURL(sourceHuangju), row["expiresAt"])
	if err != nil {
		return providerMedia{}, err
	}
	media := providerMedia{URL: address, Referer: credentials.referer, credentials: credentials}
	parsed, _ := url.Parse(address)
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		media.Playlist, media.URL, err = d.fetchMediaPlaylist(providerMediaContext(ctx, credentials), media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("获取剧果播放列表失败：%w", err)
		}
		media.Duration = m3u8Duration(media.Playlist)
	}
	return media, nil
}
