package core

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const duanjuMaoguoKey = "d3dGiJc651gSQ8w1"

var duanjuMaoguoCharMap = map[rune]rune{
	'+': 'P', '/': 'X', '0': 'M', '1': 'U', '2': 'l', '3': 'E', '4': 'r', '5': 'Y', '6': 'W', '7': 'b', '8': 'd', '9': 'J',
	'A': '9', 'B': 's', 'C': 'a', 'D': 'I', 'E': '0', 'F': 'o', 'G': 'y', 'H': '_', 'I': 'H', 'J': 'G', 'K': 'i', 'L': 't',
	'M': 'g', 'N': 'N', 'O': 'A', 'P': '8', 'Q': 'F', 'R': 'k', 'S': '3', 'T': 'h', 'U': 'f', 'V': 'R', 'W': 'q', 'X': 'C',
	'Y': '4', 'Z': 'p', 'a': 'm', 'b': 'B', 'c': 'O', 'd': 'u', 'e': 'c', 'f': '6', 'g': 'K', 'h': 'x', 'i': '5', 'j': 'T',
	'k': '-', 'l': '2', 'm': 'z', 'n': 'S', 'o': 'Z', 'p': '1', 'q': 'V', 'r': 'v', 's': 'j', 't': 'Q', 'u': '7', 'v': 'D',
	'w': 'w', 'x': 'n', 'y': 'L', 'z': 'e',
}

func duanjuMD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func duanjuMaoguoHeaders() (map[string]string, error) {
	session := strconv.FormatInt(time.Now().UnixMilli(), 10)
	payload := map[string]any{
		"static_score":  "0.8",
		"uuid":          "00000000-7fc7-08dc-0000-000000000000",
		"device-id":     "20250220125449b9b8cac84c2dd3d035c9052a2572f7dd0122edde3cc42a70",
		"mac":           "",
		"sourceuid":     "aa7de295aad621a6",
		"refresh-type":  "0",
		"model":         "22021211RC",
		"wlb-imei":      "",
		"client-id":     "aa7de295aad621a6",
		"brand":         "Redmi",
		"oaid":          "",
		"oaid-no-cache": "",
		"sys-ver":       "12",
		"trusted-id":    "",
		"phone-level":   "H",
		"imei":          "",
		"wlb-uid":       "aa7de295aad621a6",
		"session-id":    session,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	base := base64.StdEncoding.EncodeToString(encoded)
	mapped := make([]rune, 0, len(base))
	for _, char := range base {
		if replacement, found := duanjuMaoguoCharMap[char]; found {
			mapped = append(mapped, replacement)
			continue
		}
		mapped = append(mapped, char)
	}
	qmParams := string(mapped)
	signature := duanjuMD5("AUTHORIZATION=" +
		"app-version=10001" +
		"application-id=com.duoduo.read" +
		"channel=unknown" +
		"is-white=" +
		"net-env=5" +
		"platform=android" +
		"qm-params=" + qmParams +
		"reg=" + duanjuMaoguoKey)
	return map[string]string{
		"net-env":          "5",
		"reg":              "",
		"channel":          "unknown",
		"is-white":         "",
		"platform":         "android",
		"application-id":   "com.duoduo.read",
		"authorization":    "",
		"app-version":      "10001",
		"user-agent":       "webviewversion/0",
		"qm-params":        qmParams,
		"sign":             signature,
		"Accept":           "application/json",
		"X-Requested-With": "com.duoduo.read",
	}, nil
}

func maoguoDramaFromNode(node map[string]any, base string) Drama {
	id := duanjuFindString(node, "playlet_id", "playletId", "id")
	if id == "" {
		return Drama{}
	}
	total := duanjuFindInt(node, "total_episode_num", "totalEpisodeNum", "total")
	tags := duanjuStringList(node["tags"])
	if len(tags) == 0 {
		tags = duanjuStringList(node["tag_list"])
	}
	return Drama{
		ID:           providerDramaID(sourceMaoguo, id),
		Source:       sourceMaoguo,
		SourceID:     id,
		Title:        duanjuPlainText(duanjuFindString(node, "title", "playlet_name", "name")),
		Intro:        duanjuPlainText(duanjuFindString(node, "intro", "description", "desc")),
		Cover:        duanjuCover(base, node, "image_link", "imageLink", "cover", "cover_url"),
		EpisodeCount: json.Number(strconv.Itoa(total)),
		Category:     duanjuCSV(tags),
		Tags:         tags,
		ChannelName:  duanjuSourceName(sourceMaoguo),
	}
}

func (d *Downloader) fetchMaoguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	headers, err := duanjuMaoguoHeaders()
	if err != nil {
		return nil, false, err
	}
	base := d.duanjuBaseURL(sourceMaoguo)
	tagID := strings.TrimSpace(category)
	if tagID != "" && !webProviderNumericID.MatchString(tagID) {
		tagID = "0"
	}
	if tagID == "" {
		tagID = "0"
	}
	if page > 1 {
		signature := duanjuMD5("next_id=" + strconv.Itoa(page) + "operation=1playlet_privacy=1tag_id=" + tagID + duanjuMaoguoKey)
		address := fmt.Sprintf("%s/api/v1/playlet/index?tag_id=%s&playlet_privacy=1&operation=1&next_id=%d&sign=%s",
			base, tagID, page, signature)
		response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceMaoguo, Address: address, Referer: base + "/", Headers: headers})
		if err != nil {
			return nil, false, err
		}
		items := maoguoCatalogItems(duanjuFindMap(response, "data"), base)
		return items, len(items) > 0, nil
	}
	signature := duanjuMD5("operation=1playlet_privacy=1tag_id=" + tagID + duanjuMaoguoKey)
	address := fmt.Sprintf("%s/api/v1/playlet/index?tag_id=%s&playlet_privacy=1&operation=1&sign=%s", base, tagID, signature)
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceMaoguo, Address: address, Referer: base + "/", Headers: headers})
	if err != nil {
		return nil, false, err
	}
	items := maoguoCatalogItems(duanjuFindMap(response, "data"), base)
	return items, false, nil
}

func maoguoCatalogItems(data map[string]any, base string) []Drama {
	var items []Drama
	for _, entry := range duanjuFindList(data, "list") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := maoguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items
}

func (d *Downloader) fetchMaoguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	headers, err := duanjuMaoguoHeaders()
	if err != nil {
		return Drama{}, nil, err
	}
	base := d.duanjuBaseURL(sourceMaoguo)
	signature := duanjuMD5("playlet_id=" + sourceID + duanjuMaoguoKey)
	address := fmt.Sprintf("%s/player/api/v1/playlet/info?playlet_id=%s&sign=%s", strings.TrimRight(maoguoReadURL, "/"), url.QueryEscape(sourceID), signature)
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceMaoguo, Address: address, Referer: base + "/", Headers: headers})
	if err != nil {
		return Drama{}, nil, err
	}
	data := duanjuFindMap(response, "data")
	if data == nil {
		return Drama{}, nil, errors.New("猫果未返回剧集资料")
	}
	drama := maoguoDramaFromNode(data, base)
	drama.ID, drama.Source, drama.SourceID = providerDramaID(sourceMaoguo, sourceID), sourceMaoguo, sourceID
	var chapters []Chapter
	for index, entry := range duanjuFindList(data, "play_list") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoURL := duanjuFindString(node, "video_url", "videoUrl", "url")
		if videoURL == "" {
			continue
		}
		number := duanjuFindInt(node, "sort", "episode", "index")
		if number <= 0 {
			number = index + 1
		}
		chapters = append(chapters, duanjuChapter(sourceMaoguo, sourceID, number,
			duanjuFindString(node, "title", "name"), videoURL, "", base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("猫果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	return drama, chapters, nil
}

const duanjuMaoguoTrackID = "ec1280db127955061754851657967"

func (d *Downloader) searchMaoguo(ctx context.Context, query string) ([]Drama, error) {
	headers, err := duanjuMaoguoHeaders()
	if err != nil {
		return nil, err
	}
	base := d.duanjuBaseURL(sourceMaoguo)
	signature := duanjuMD5(fmt.Sprintf("extend=page=1read_preference=0track_id=%swd=%s%s", duanjuMaoguoTrackID, query, duanjuMaoguoKey))
	address := fmt.Sprintf("%s/api/v1/playlet/search?extend=&page=1&wd=%s&read_preference=0&track_id=%s&sign=%s", base, url.QueryEscape(query), duanjuMaoguoTrackID, signature)
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceMaoguo, Address: address, Referer: base + "/", Headers: headers})
	if err != nil {
		return nil, err
	}
	return maoguoCatalogItems(duanjuFindMap(response, "data"), base), nil
}

func (d *Downloader) fetchMaoguoCategories(ctx context.Context) ([]nativeCategory, error) {
	headers, err := duanjuMaoguoHeaders()
	if err != nil {
		return nil, err
	}
	base := d.duanjuBaseURL(sourceMaoguo)
	signature := duanjuMD5("operation=1" + duanjuMaoguoKey)
	address := fmt.Sprintf("%s/api/v1/playlet/tag/list?operation=1&sign=%s", base, signature)
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceMaoguo, Address: address, Referer: base + "/", Headers: headers})
	if err != nil {
		return nil, err
	}
	var categories []nativeCategory
	for _, entry := range duanjuFindList(duanjuFindMap(response, "data"), "list") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id := duanjuFindString(node, "tag_id", "id")
		name := duanjuFindString(node, "tag_name", "name", "title")
		if id == "" || name == "" || len([]rune(name)) > 24 {
			continue
		}
		categories = append(categories, nativeCategory{ID: id, Name: name})
	}
	return categories, nil
}

func guanguoClientInfo() string {
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if len(stamp) > 10 {
		stamp = stamp[len(stamp)-10:]
	}
	return duanjuMD5(stamp)
}

func guanguoQuery(values url.Values) url.Values {
	query := url.Values{
		"version_code":       {"1500"},
		"version_name":       {"1.5.0"},
		"device_name":        {"Pixel 8 Pro"},
		"device_type":        {"phone"},
		"is_first_day":       {"true"},
		"is_first_24h":       {"true"},
		"app_launch_way":     {"icon"},
		"default_homepage":   {"homepage_interaction"},
		"device_owning_firm": {"Google"},
		"font_scale":         {"default"},
		"os_type":            {"1"},
		"clientInfo":         {guanguoClientInfo()},
	}
	for key, entries := range values {
		query[key] = entries
	}
	return query
}

func guanguoDramaFromNode(node map[string]any, base string) Drama {
	id := duanjuFindString(node, "oneId", "one_id", "id")
	if id == "" {
		return Drama{}
	}
	tags := duanjuStringList(node["shortPlayTag"])
	return Drama{
		ID:           providerDramaID(sourceGuanguo, id),
		Source:       sourceGuanguo,
		SourceID:     id,
		Title:        duanjuFindString(node, "title", "name"),
		Intro:        duanjuFindString(node, "description", "desc"),
		Cover:        duanjuCover(base, node, "horzPoster", "vertPoster", "cover"),
		EpisodeCount: json.Number(strconv.Itoa(duanjuFindInt(node, "episodeCount", "totalEpisodeCount"))),
		Category:     duanjuCSV(tags),
		Tags:         tags,
		Views:        duanjuViewsText(duanjuFindInt(node, "viewCount")),
		ChannelName:  duanjuSourceName(sourceGuanguo),
	}
}

func duanjuViewsText(count int) string {
	if count <= 0 {
		return ""
	}
	if count >= 10000 {
		return strconv.FormatFloat(float64(count)/10000, 'f', 1, 64) + "万次播放"
	}
	return strconv.Itoa(count) + "次播放"
}

func (d *Downloader) fetchGuanguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	if page > 500 {
		return nil, false, nil
	}
	base := d.duanjuBaseURL(sourceGuanguo)
	address := base + "/drama/home/search?" + guanguoQuery(nil).Encode()
	payload, err := json.Marshal(map[string]any{
		"audience":   "全部",
		"order":      "最新",
		"page":       page,
		"pageSize":   30,
		"searchWord": "",
		"subject":    strings.TrimSpace(category),
	})
	if err != nil {
		return nil, false, err
	}
	response, err := d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceGuanguo,
		Method:  http.MethodPost,
		Address: address,
		Referer: base + "/",
		Agent:   "okhttp/5.1.0",
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
		Body:    payload,
	})
	if err != nil {
		return nil, false, err
	}
	var items []Drama
	for _, entry := range duanjuFindList(response, "data") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := guanguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, len(items) >= 30, nil
}

func (d *Downloader) fetchGuanguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceGuanguo)
	query := guanguoQuery(url.Values{
		"oneId":    {sourceID},
		"page":     {"1"},
		"pageSize": {"1000"},
		"userId":   {"0"},
		"queryAll": {"true"},
	})
	address := base + "/drama/home/shortVideoDetail?" + query.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceGuanguo,
		Address: address,
		Referer: base + "/",
		Agent:   "okhttp/5.1.0",
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
	})
	if err != nil {
		return Drama{}, nil, err
	}
	title := duanjuFindString(response, "title")
	if title == "" && duanjuFindList(response, "data") == nil {
		return Drama{}, nil, errors.New("观果详情接口当前返回空数据，该源暂时无法查看分集，请稍后重试或改用其他站源")
	}
	drama := Drama{
		ID:          providerDramaID(sourceGuanguo, sourceID),
		Source:      sourceGuanguo,
		SourceID:    sourceID,
		Title:       title,
		Intro:       duanjuFindString(response, "description"),
		Cover:       duanjuCover(base, response, "vertPoster", "horzPoster"),
		Category:    duanjuCSV(duanjuStringList(response["shortPlayTag"])),
		Tags:        duanjuStringList(response["shortPlayTag"]),
		ChannelName: duanjuSourceName(sourceGuanguo),
		Views:       duanjuViewsText(duanjuFindInt(response, "viewedEpisodeNumber")),
	}
	var chapters []Chapter
	for index, entry := range duanjuFindList(response, "data") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoURL := guanguoEpisodeURL(node)
		if videoURL == "" {
			continue
		}
		number := duanjuFindInt(node, "playOrder", "episodeOrder", "index", "episode")
		if number <= 0 {
			number = index + 1
		}
		chapters = append(chapters, duanjuChapter(sourceGuanguo, sourceID, number,
			duanjuFindString(node, "title", "name"), videoURL, "", base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("观果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	drama.EpisodeCount = json.Number(strconv.Itoa(len(chapters)))
	if total := duanjuFindInt(response, "totalEpisodeCount"); total > len(chapters) {
		drama.EpisodeCount = json.Number(strconv.Itoa(total))
	}
	return drama, chapters, nil
}

func guanguoEpisodeURL(node map[string]any) string {
	if direct := duanjuFindString(node, "playUrl", "videoUrl", "play_url"); direct != "" {
		return direct
	}
	clarities := duanjuFindList(node, "videoClarityList")
	best := ""
	bestQuality := -1
	for _, entry := range clarities {
		clarity, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		address := duanjuFindString(clarity, "playUrl", "videoUrl", "url")
		if address == "" {
			continue
		}
		quality := duanjuFindInt(clarity, "clarity", "quality", "definition")
		if quality > bestQuality {
			best, bestQuality = address, quality
		}
	}
	return best
}

func (d *Downloader) searchGuanguo(ctx context.Context, query string) ([]Drama, error) {
	base := d.duanjuBaseURL(sourceGuanguo)
	address := base + "/drama/home/search?" + guanguoQuery(nil).Encode()
	payload, err := json.Marshal(map[string]any{
		"audience":   "全部",
		"order":      "最新",
		"page":       1,
		"pageSize":   30,
		"searchWord": query,
		"subject":    "",
	})
	if err != nil {
		return nil, err
	}
	response, err := d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceGuanguo,
		Method:  http.MethodPost,
		Address: address,
		Referer: base + "/",
		Agent:   "okhttp/5.1.0",
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
		Body:    payload,
	})
	if err != nil {
		return nil, err
	}
	var items []Drama
	for _, entry := range duanjuFindList(response, "data") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := guanguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, nil
}
