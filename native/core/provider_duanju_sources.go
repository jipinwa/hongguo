package core

import (
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func fanguoDramaFromNode(node map[string]any, base string) Drama {
	id := duanjuFindString(node, "duanjuId", "duanju_id", "id")
	if id == "" {
		return Drama{}
	}
	source := duanjuFindString(node, "source")
	sourceKey := id
	if source != "" {
		sourceKey = id + "#" + source
	}
	tags := duanjuStringList(node["categories"])
	return Drama{
		ID:           providerDramaID(sourceFanguo, sourceKey),
		Source:       sourceFanguo,
		SourceID:     sourceKey,
		Title:        duanjuFindString(node, "title", "name"),
		Intro:        duanjuFindString(node, "description", "desc", "intro"),
		Cover:        duanjuCover(base, node, "coverImageUrl", "cover_image_url", "cover"),
		EpisodeCount: json.Number(strconv.Itoa(duanjuFindInt(node, "total", "totalEpisodeNum"))),
		Category:     duanjuCSV(tags),
		Tags:         tags,
		ChannelName:  duanjuSourceName(sourceFanguo),
	}
}

const fanguoDefaultKeyword = "都市"

func (d *Downloader) fetchFanguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	if page > 1 {
		return nil, false, nil
	}
	base := d.duanjuBaseURL(sourceFanguo)
	keyword := strings.TrimSpace(category)
	if keyword == "" {
		keyword = fanguoDefaultKeyword
	}
	address := base + "/xifan/search/getSearchList?" + url.Values{
		"reqType":            {"search"},
		"offset":             {"0"},
		"keyword":            {keyword},
		"quickEngineVersion": {"-1"},
		"scene":              {""},
	}.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceFanguo, Address: address, Referer: base + "/"})
	if err != nil {
		return nil, false, err
	}
	var items []Drama
	seen := map[string]bool{}
	for _, block := range duanjuFindList(duanjuFindMap(response, "result"), "elements") {
		mapped, ok := block.(map[string]any)
		if !ok {
			continue
		}
		for _, entry := range duanjuFindList(mapped, "contents") {
			content, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			drama := fanguoDramaFromNode(duanjuFindMap(content, "duanjuVo"), base)
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			items = append(items, drama)
		}
	}
	return items, false, nil
}

func (d *Downloader) fetchFanguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceFanguo)
	duanjuID, source := sourceID, ""
	if id, rest, found := strings.Cut(sourceID, "#"); found {
		duanjuID, source = id, rest
	}
	address := base + "/xifan/drama/getDuanjuInfo?" + url.Values{
		"duanjuId": {duanjuID},
		"source":   {source},
	}.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceFanguo, Address: address, Referer: base + "/"})
	if err != nil {
		return Drama{}, nil, err
	}
	data := duanjuFindMap(response, "result")
	if data == nil {
		return Drama{}, nil, errors.New("饭果未返回剧集资料")
	}
	drama := Drama{
		ID:          providerDramaID(sourceFanguo, sourceID),
		Source:      sourceFanguo,
		SourceID:    sourceID,
		Title:       duanjuFindString(data, "title", "name"),
		Intro:       duanjuFindString(data, "description", "desc", "intro"),
		Cover:       duanjuCover(base, data, "coverImageUrl", "cover_image_url", "cover"),
		Category:    duanjuCSV(duanjuStringList(data["categories"])),
		Tags:        duanjuStringList(data["categories"]),
		ChannelName: duanjuSourceName(sourceFanguo),
		ReleaseStatus: duanjuReleaseStatus(
			duanjuFindString(data, "updateStatus"),
			duanjuFindInt(data, "total"), duanjuFindInt(data, "current"),
		),
	}
	var chapters []Chapter
	for index, entry := range duanjuFindList(data, "episodeList") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoURL := duanjuFindString(node, "playUrl", "play_url", "videoUrl")
		if videoURL == "" {
			continue
		}
		number := duanjuFindInt(node, "index", "episode", "sort")
		if number <= 0 {
			number = index + 1
		}
		chapters = append(chapters, duanjuChapter(sourceFanguo, sourceID, number,
			duanjuFindString(node, "title", "name"), videoURL, "", base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("饭果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	return drama, chapters, nil
}

func (d *Downloader) searchFanguo(ctx context.Context, query string) ([]Drama, error) {
	base := d.duanjuBaseURL(sourceFanguo)
	address := base + "/xifan/search/getSearchList?" + url.Values{
		"reqType":            {"search"},
		"offset":             {"0"},
		"keyword":            {query},
		"quickEngineVersion": {"-1"},
		"scene":              {""},
	}.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceFanguo, Address: address, Referer: base + "/"})
	if err != nil {
		return nil, err
	}
	var items []Drama
	seen := map[string]bool{}
	for _, block := range duanjuFindList(duanjuFindMap(response, "result"), "elements") {
		mapped, ok := block.(map[string]any)
		if !ok {
			continue
		}
		for _, entry := range duanjuFindList(mapped, "contents") {
			content, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			drama := fanguoDramaFromNode(duanjuFindMap(content, "duanjuVo"), base)
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			items = append(items, drama)
		}
	}
	return items, nil
}

const (
	xingguoProductID = "2a8c14d1-72e7-498b-af23-381028eb47c0"
	xingguoVestID    = "2be070e0-c824-4d0e-a67a-8f688890cadb"
	xingguoToken     = "202509271001001446030204698626"
)

func xingguoQuery(resourceID, page, size string) url.Values {
	return url.Values{
		"productId":  {xingguoProductID},
		"vestId":     {xingguoVestID},
		"channel":    {"oppo19"},
		"osType":     {"android"},
		"version":    {"20"},
		"token":      {xingguoToken},
		"resourceId": {resourceID},
		"pageNum":    {page},
		"pageSize":   {size},
	}
}

func xingguoDramaFromNode(node map[string]any, base string) Drama {
	id := duanjuFindString(node, "id", "bookId")
	if id == "" {
		return Drama{}
	}
	// 星果把简介编码在 ID 中以携带到详情，此处沿用脚本约定。
	sourceKey := id
	if intro := duanjuFindString(node, "introduction"); intro != "" {
		sourceKey = id + "@" + intro
	}
	heat := duanjuFindInt(node, "heat")
	views := ""
	if heat > 0 {
		views = strconv.Itoa(heat) + "万播放"
	}
	return Drama{
		ID:           providerDramaID(sourceXingguo, sourceKey),
		Source:       sourceXingguo,
		SourceID:     sourceKey,
		Title:        duanjuFindString(node, "name", "title"),
		Intro:        duanjuFindString(node, "introduction", "desc"),
		Cover:        duanjuCover(base, node, "icon", "cover"),
		EpisodeCount: json.Number(strconv.Itoa(duanjuFindInt(node, "chapterCount", "total"))),
		Views:        views,
		ChannelName:  duanjuSourceName(sourceXingguo),
	}
}

func (d *Downloader) fetchXingguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	base := d.duanjuBaseURL(sourceXingguo)
	resourceID := strings.TrimSpace(category)
	if resourceID == "" {
		resourceID = duanjuStaticCategories[sourceXingguo][0].ID
	}
	if !webProviderNumericID.MatchString(resourceID) {
		return nil, false, errors.New("星果分类无效")
	}
	address := base + "/novel-api/app/pageModel/getResourceById?" + xingguoQuery(resourceID, strconv.Itoa(page), "10").Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceXingguo, Address: address, Referer: base + "/"})
	if err != nil {
		return nil, false, err
	}
	data := duanjuFindMap(response, "data")
	var items []Drama
	for _, entry := range duanjuFindList(data, "datalist") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := xingguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items, len(items) >= 10, nil
}

func (d *Downloader) searchXingguo(ctx context.Context, query string) ([]Drama, error) {
	keyword := strings.TrimSpace(query)
	if keyword == "" {
		return nil, errors.New("请输入搜索关键词")
	}
	base := d.duanjuBaseURL(sourceXingguo)
	values := url.Values{
		"productId": {xingguoProductID},
		"vestId":    {xingguoVestID},
		"channel":   {"oppo19"},
		"osType":    {"android"},
		"version":   {"20"},
		"token":     {xingguoToken},
		"key":       {keyword},
		"pageNum":   {"1"},
		"pageSize":  {"20"},
	}
	address := base + "/novel-api/basedata/book/searchBook?" + values.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceXingguo, Address: address, Referer: base + "/"})
	if err != nil {
		return nil, err
	}
	data := duanjuFindMap(response, "data")
	var items []Drama
	seen := map[string]bool{}
	for _, entry := range duanjuFindList(data, "datalist") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		drama := xingguoDramaFromNode(node, base)
		if drama.ID == "" || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	if len(items) == 0 {
		return nil, errors.New("星果未搜索到相关剧集")
	}
	return items, nil
}

func (d *Downloader) fetchXingguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceXingguo)
	bookID, intro, _ := strings.Cut(sourceID, "@")
	address := base + "/novel-api/basedata/book/getChapterList?" + url.Values{
		"bookId":    {bookID},
		"productId": {xingguoProductID},
		"vestId":    {xingguoVestID},
		"channel":   {"oppo19"},
		"osType":    {"android"},
		"version":   {"20"},
		"token":     {xingguoToken},
	}.Encode()
	response, err := d.duanjuJSON(ctx, duanjuRequest{Source: sourceXingguo, Address: address, Referer: base + "/"})
	if err != nil {
		return Drama{}, nil, err
	}
	drama := Drama{
		ID:          providerDramaID(sourceXingguo, sourceID),
		Source:      sourceXingguo,
		SourceID:    sourceID,
		Intro:       intro,
		ChannelName: duanjuSourceName(sourceXingguo),
	}
	var chapters []Chapter
	for index, entry := range duanjuFindList(response, "data") {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		videoURL := xingguoEpisodeURL(node)
		if videoURL == "" {
			continue
		}
		number := index + 1
		if parsed := duanjuFindInt(node, "chapterNum", "sort", "index"); parsed > 0 {
			number = parsed
		}
		chapters = append(chapters, duanjuChapter(sourceXingguo, sourceID, number, "", videoURL, "", base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("星果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	drama.EpisodeCount = json.Number(strconv.Itoa(len(chapters)))
	return drama, chapters, nil
}

func xingguoEpisodeURL(node map[string]any) string {
	if direct := duanjuFindString(node, "shortPlayUrl", "playUrl", "url"); direct != "" {
		return direct
	}
	for _, listKey := range []string{"shortPlayList", "chapterShortPlayVoList"} {
		for _, entry := range duanjuFindList(node, listKey) {
			child, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if address := xingguoEpisodeURL(child); address != "" {
				return address
			}
		}
	}
	return ""
}

func niuguoDramaFromNode(node map[string]any, base string) Drama {
	id := duanjuFindString(node, "vod_id", "vodId", "id")
	if id == "" {
		return Drama{}
	}
	tags := duanjuStringList(node["vod_tag"])
	if len(tags) == 0 && duanjuFindString(node, "vod_tag") != "" {
		tags = strings.Split(duanjuFindString(node, "vod_tag"), ",")
	}
	return Drama{
		ID:           providerDramaID(sourceNiuguo, id),
		Source:       sourceNiuguo,
		SourceID:     id,
		Title:        duanjuFindString(node, "vod_name", "vodName", "title"),
		Intro:        duanjuFindString(node, "vod_content", "vod_blurb", "vodContent", "desc"),
		Cover:        duanjuCover(base, node, "vod_pic", "vodPic", "cover"),
		EpisodeCount: json.Number(strconv.Itoa(duanjuFindInt(node, "vod_total", "total", "episodes"))),
		Category:     duanjuFindString(node, "type_name", "typeName"),
		Tags:         tags,
		Remark:       duanjuFindString(node, "vod_remarks", "vodRemarks"),
		ChannelName:  duanjuSourceName(sourceNiuguo),
	}
}

func (d *Downloader) fetchNiuguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	base := d.duanjuBaseURL(sourceNiuguo)
	query := url.Values{
		"class":   {strings.TrimSpace(category)},
		"order":   {"最新"},
		"type_id": {"5"},
		"area":    {""},
		"year":    {""},
		"state":   {""},
		"wd":      {""},
		"page":    {strconv.Itoa(page)},
	}
	response, err := d.fetchNiuguoJSON(ctx, "/list", query)
	if err != nil {
		return nil, false, err
	}
	items := niuguoCatalogItems(response, base)
	return items, len(items) > 0, nil
}

func niuguoCatalogItems(response map[string]any, base string) []Drama {
	rows := duanjuFindList(response, "list", "data")
	if rows == nil {
		rows = duanjuFindList(duanjuFindMap(response, "data"), "list")
	}
	var items []Drama
	for _, entry := range rows {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if drama := niuguoDramaFromNode(node, base); drama.ID != "" {
			items = append(items, drama)
		}
	}
	return items
}

func (d *Downloader) fetchNiuguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceNiuguo)
	response, err := d.fetchNiuguoJSON(ctx, "/detail", url.Values{"vod_id": {sourceID}})
	if err != nil {
		return Drama{}, nil, err
	}
	node := response
	if nested := duanjuFindMap(response, "data"); nested != nil {
		node = nested
	}
	if listed := duanjuFindList(node, "list"); len(listed) > 0 {
		if first, ok := listed[0].(map[string]any); ok {
			node = first
		}
	}
	drama := niuguoDramaFromNode(node, base)
	drama.ID, drama.Source, drama.SourceID = providerDramaID(sourceNiuguo, sourceID), sourceNiuguo, sourceID
	var chapters []Chapter
	seen := map[int]bool{}
	appendEpisode := func(title, link string) {
		link = strings.TrimSpace(link)
		if link == "" {
			return
		}
		number := duanjuEpisodeNumber(title, len(chapters)+1)
		if seen[number] {
			return
		}
		seen[number] = true
		chapters = append(chapters, duanjuChapter(sourceNiuguo, sourceID, number, title, link, "", base+"/"))
	}
	for _, entry := range duanjuFindList(node, "sources") {
		source, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, item := range duanjuFindList(source, "episodes") {
			episode, ok := item.(map[string]any)
			if !ok {
				continue
			}
			appendEpisode(duanjuFindString(episode, "name", "title"), duanjuFindString(episode, "url", "playUrl"))
		}
	}
	if len(chapters) == 0 {
		for _, entry := range duanjuFindList(node, "vod_play_url") {
			for _, line := range strings.Split(nativeText(entry), "#") {
				if title, link, found := strings.Cut(strings.TrimSpace(line), "$"); found {
					appendEpisode(title, link)
				}
			}
		}
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("牛果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	return drama, chapters, nil
}

func (d *Downloader) searchNiuguo(ctx context.Context, query string) ([]Drama, error) {
	base := d.duanjuBaseURL(sourceNiuguo)
	response, err := d.fetchNiuguoJSON(ctx, "/list", url.Values{
		"class":   {""},
		"order":   {"最新"},
		"type_id": {"5"},
		"area":    {""},
		"year":    {""},
		"state":   {""},
		"wd":      {query},
		"page":    {"1"},
	})
	if err != nil {
		return nil, err
	}
	return niuguoCatalogItems(response, base), nil
}

// 牛果分集的 url 字段是解析服务需要的内部分集 ID，不是可播放地址。
// 参考脚本依次尝试 qy.php、dj.php 和备用解析域名，任一返回有效地址即采用。
func (d *Downloader) resolveNiuguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	sourceID := strings.TrimSpace(task.Chapter.VideoURL)
	if sourceID == "" {
		return providerMedia{}, errors.New("牛果分集缺少解析 ID，请刷新详情后重试")
	}
	name := duanjuSourceName(sourceNiuguo)
	var lastErr error
	for _, base := range []string{niuguoParseURL, niuguoParseURL2} {
		for _, script := range []string{"qy.php", "dj.php"} {
			address := fmt.Sprintf("%s/jx/%s?url=%s", strings.TrimRight(base, "/"), script, url.QueryEscape(sourceID))
			body, err := d.fetchProviderText(ctx, address, base+"/")
			if err != nil {
				lastErr = err
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(body), &payload) != nil {
				lastErr = errors.New("牛果解析服务返回的数据格式无法识别")
				continue
			}
			resolved := duanjuFindString(payload, "url")
			if resolved == "" {
				lastErr = errors.New("牛果解析服务未返回播放地址")
				continue
			}
			return d.prepareWebProviderMedia(ctx, providerMedia{URL: resolved, Referer: base + "/"}, name)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s未返回有效播放地址，请刷新章节后重试", name)
	}
	return providerMedia{}, lastErr
}

func duanjuFormatCount(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d集", value)
}

// 牛果按请求 URI 的前 16 字节派生 AES-ECB 密钥，密文为 Base64。
// 密钥必须用实际发出的 URI 计算，参数顺序变化会得到不同密钥。
func niuguoDecryptKey(requestURI string) []byte {
	padded := make([]byte, 16)
	copy(padded, requestURI)
	return padded
}

func niuguoRequestURI(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}
	uri := parsed.EscapedPath()
	if parsed.RawQuery != "" {
		uri += "?" + parsed.RawQuery
	}
	return uri
}

func niuguoDecryptResponse(payload []byte, key []byte) (map[string]any, error) {
	encoded := strings.TrimSpace(string(payload))
	if encoded == "" {
		return nil, errors.New("牛果返回了空响应")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		if decoded, plainErr := base64.RawStdEncoding.DecodeString(encoded); plainErr == nil {
			raw, err = decoded, nil
		}
	}
	if err != nil {
		return nil, errors.New("牛果响应不是有效的 Base64 数据")
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, errors.New("牛果响应长度不符合加密块要求")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(raw))
	for offset := 0; offset < len(raw); offset += aes.BlockSize {
		block.Decrypt(plain[offset:offset+aes.BlockSize], raw[offset:offset+aes.BlockSize])
	}
	unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, errors.New("牛果响应解密失败")
	}
	var decoded map[string]any
	if json.Unmarshal(unpadded, &decoded) != nil {
		return nil, errors.New("牛果响应解密后不是有效数据")
	}
	return decoded, nil
}

func (d *Downloader) fetchNiuguoJSON(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	base := d.duanjuBaseURL(sourceNiuguo)
	address := base + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	payload, err := d.duanjuDo(ctx, duanjuRequest{Source: sourceNiuguo, Address: address, Referer: base + "/"})
	if err != nil {
		return nil, err
	}
	return niuguoDecryptResponse(payload, niuguoDecryptKey(niuguoRequestURI(address)))
}
