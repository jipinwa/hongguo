package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	nextDataPattern    = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
	nextEpisodePattern = regexp.MustCompile(`(?i)(https?://[^"'\s<>\\]+\.(?:mp4|m3u8)[^"'\s<>\\]*)`)
)

func nextPageProps(body string) map[string]any {
	matches := nextDataPattern.FindStringSubmatch(body)
	if len(matches) < 2 {
		return nil
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(matches[1]), &decoded) != nil {
		return nil
	}
	props, _ := decoded["props"].(map[string]any)
	pageProps, _ := props["pageProps"].(map[string]any)
	return pageProps
}

func heguoDramaFromBook(book map[string]any, base string) Drama {
	id := duanjuFindString(book, "bookId", "book_id", "id")
	if id == "" {
		return Drama{}
	}
	status := duanjuFindString(book, "statusDesc")
	total := duanjuFindInt(book, "totalChapterNum")
	remark := strings.TrimSpace(status + " " + duanjuFormatCount(total))
	return Drama{
		ID:            providerDramaID(sourceHeguo, id),
		Source:        sourceHeguo,
		SourceID:      id,
		Title:         duanjuFindString(book, "bookName", "title", "name"),
		Intro:         duanjuFindString(book, "introduction", "intro", "description"),
		Cover:         duanjuCover(base, book, "coverWap", "cover", "coverUrl"),
		Remark:        remark,
		EpisodeCount:  json.Number(strconv.Itoa(total)),
		ChannelName:   duanjuSourceName(sourceHeguo),
		ReleaseStatus: duanjuReleaseStatus(status, total, 0),
	}
}

func heguoBooks(pageProps map[string]any, base string) []Drama {
	var items []Drama
	seen := map[string]bool{}
	appendBooks := func(rows []any) {
		for _, entry := range rows {
			book, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			drama := heguoDramaFromBook(book, base)
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			items = append(items, drama)
		}
	}
	for _, key := range []string{"bannerList", "bookList", "recommendList"} {
		appendBooks(duanjuFindList(pageProps, key))
	}
	for _, entry := range duanjuFindList(pageProps, "seoColumnVos") {
		column, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		appendBooks(duanjuFindList(column, "bookInfos"))
	}
	return items
}

func (d *Downloader) fetchHeguoCatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	base := d.duanjuBaseURL(sourceHeguo)
	address := base + "/"
	if class := strings.TrimSpace(category); class != "" {
		address = base + "/browse/" + url.PathEscape(class) + "/" + strconv.Itoa(page)
	}
	body, err := d.fetchProviderText(ctx, address, base+"/")
	if err != nil {
		return nil, false, err
	}
	pageProps := nextPageProps(body)
	if pageProps == nil {
		return nil, false, errors.New("河果页面结构无法识别")
	}
	items := heguoBooks(pageProps, base)
	totalPages := duanjuFindInt(pageProps, "pages")
	current := duanjuFindInt(pageProps, "page")
	if current <= 0 {
		current = page
	}
	return items, totalPages > current, nil
}

func (d *Downloader) fetchHeguoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(sourceHeguo)
	address := base + "/drama/" + url.PathEscape(sourceID)
	body, err := d.fetchProviderText(ctx, address, base+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	pageProps := nextPageProps(body)
	if pageProps == nil {
		return Drama{}, nil, errors.New("河果详情页结构无法识别")
	}
	book := duanjuFindMap(pageProps, "bookInfoVo")
	if book == nil {
		return Drama{}, nil, errors.New("河果未返回剧集资料")
	}
	drama := heguoDramaFromBook(book, base)
	drama.ID, drama.Source, drama.SourceID = providerDramaID(sourceHeguo, sourceID), sourceHeguo, sourceID
	drama.Category = duanjuCSV(duanjuStringList(book["categoryList"]))
	drama.Tags = duanjuStringList(book["categoryList"])
	if performers := duanjuStringList(book["performerList"]); len(performers) > 0 {
		drama.Remark = strings.TrimSpace(drama.Remark)
	}
	var chapters []Chapter
	for index, entry := range duanjuFindList(pageProps, "chapterList") {
		chapter, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		chapterID := duanjuFindString(chapter, "chapterId", "chapter_id", "id")
		title := duanjuFindString(chapter, "chapterName", "title")
		if chapterID == "" {
			continue
		}
		number := duanjuFindInt(chapter, "chapterNum", "sort", "index")
		if number <= 0 {
			number = index + 1
		}
		pageURL := base + "/episode/" + url.PathEscape(sourceID) + "/" + url.PathEscape(chapterID)
		chapters = append(chapters, duanjuChapter(sourceHeguo, sourceID, number, title, "", pageURL, base+"/"))
	}
	if len(chapters) == 0 {
		return drama, nil, errors.New("河果未返回可播放分集")
	}
	sortDuanjuChapters(chapters)
	drama.EpisodeCount = json.Number(strconv.Itoa(len(chapters)))
	return drama, chapters, nil
}

func (d *Downloader) searchHeguo(ctx context.Context, query string) ([]Drama, error) {
	keyword := strings.TrimSpace(query)
	if keyword == "" {
		return nil, errors.New("请输入搜索关键词")
	}
	base := d.duanjuBaseURL(sourceHeguo)
	body, err := json.Marshal(map[string]any{
		"sourceType": heguoSearchSourceType,
		"keyword":    keyword,
		"index":      1,
	})
	if err != nil {
		return nil, err
	}
	response, err := d.duanjuJSON(ctx, duanjuRequest{
		Source:  sourceHeguo,
		Method:  http.MethodPost,
		Address: base + heguoSearchPath,
		Referer: base + "/",
		Headers: map[string]string{"Content-Type": "application/json", "pname": heguoSearchPName},
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	items := heguoSearchBooks(response, base)
	if len(items) == 0 {
		return nil, errors.New("河果未搜索到相关剧集")
	}
	return items, nil
}

func heguoSearchBooks(response map[string]any, base string) []Drama {
	data := duanjuFindMap(response, "data")
	var items []Drama
	seen := map[string]bool{}
	for _, entry := range duanjuFindList(data, "bookList") {
		book, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		drama := heguoDramaFromBook(book, base)
		if drama.ID == "" || seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	return items
}

func (d *Downloader) resolveHeguoMedia(ctx context.Context, task Task) (providerMedia, error) {
	source := canonicalProviderSource(task.Chapter.Source)
	base := d.duanjuBaseURL(source)
	pageURL := strings.TrimSpace(task.Chapter.PageURL)
	if pageURL == "" {
		return providerMedia{}, errors.New("河果分集缺少播放页面，请刷新详情后重试")
	}
	body, err := d.fetchProviderText(ctx, pageURL, base+"/")
	if err != nil {
		return providerMedia{}, err
	}
	address := ""
	if pageProps := nextPageProps(body); pageProps != nil {
		if chapter := duanjuFindMap(pageProps, "chapterInfo"); chapter != nil {
			if video := duanjuFindMap(chapter, "chapterVideoVo"); video != nil {
				for _, key := range []string{"mp4", "mp4720p", "vodMp4Url"} {
					if candidate := duanjuFindString(video, key); candidate != "" {
						address = candidate
						break
					}
				}
			}
		}
	}
	if address == "" {
		if matches := nextEpisodePattern.FindStringSubmatch(body); len(matches) > 1 {
			address = strings.ReplaceAll(matches[1], `\/`, `/`)
		}
	}
	if address == "" {
		return providerMedia{}, errors.New("河果未返回有效播放地址，请刷新章节后重试")
	}
	return d.prepareWebProviderMedia(ctx, providerMedia{
		URL:     address,
		Referer: pageURL,
	}, duanjuSourceName(source))
}
