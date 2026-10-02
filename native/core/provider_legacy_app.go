package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func (d *Downloader) fetchLegacyCatalogPage(ctx context.Context, page int) ([]Drama, bool, error) {
	return d.fetchLegacyCatalogCategoryPage(ctx, page, "")
}

func (d *Downloader) fetchLegacyCatalogCategoryPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	var tabs legacyTabList
	if err := d.fetchAPI(ctx, "/api/app/playlet-tab/all", nil, &tabs); err != nil {
		return nil, false, err
	}
	if len(tabs) == 0 {
		return nil, false, errors.New("黄果旧 API 未返回可用分类")
	}
	pageSize := max(1, d.cfg.PageSize)
	seen := map[string]bool{}
	var items []Drama
	var failures []error
	hasMore := false
	for _, tab := range tabs {
		if tab.ID == "" || category != "" && category != tab.ID {
			continue
		}
		var result listResponse
		err := d.fetchAPI(ctx, "/api/app/playlet/home/tab/"+url.PathEscape(tab.ID), map[string]string{
			"pageNumber": strconv.Itoa(page), "pageSize": strconv.Itoa(pageSize), "tabSortType": "1",
		}, &result)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", tab.Name, err))
			hasMore = true
			if ctx.Err() != nil {
				break
			}
			continue
		}
		hasMore = hasMore || len(result.List) >= pageSize
		for _, drama := range result.List {
			if drama.ID == "" || seen[drama.ID] {
				continue
			}
			seen[drama.ID] = true
			drama.SourceID, drama.Source = drama.ID, sourceCloudFront
			drama.ID = providerDramaID(sourceCloudFront, drama.SourceID)
			drama.ChannelName = tab.Name
			drama.CategoryName = firstNonEmpty(drama.CategoryName, drama.CategoryNameSnake, drama.Category, tab.Name)
			for _, cover := range []any{drama.CoverURL, drama.CoverURLSnake, drama.Cover, drama.ImageURL, drama.ImageURLSnake, drama.Image, drama.Img, drama.Pic, drama.Picture, drama.Poster, drama.Thumb, drama.Thumbnail} {
				if address := legacyCoverURL(cover); address != "" {
					drama.Cover, drama.CoverURL = address, address
					break
				}
			}
			items = append(items, drama)
		}
	}
	if category != "" {
		found := false
		for _, tab := range tabs {
			found = found || tab.ID == category
		}
		if !found {
			return nil, false, errors.New("此分类已调整，请刷新分类列表")
		}
	}
	return items, hasMore, errors.Join(failures...)
}

func (d *Downloader) fetchLegacyChapters(ctx context.Context, sourceID string) (string, []Chapter, error) {
	drama, chapters, err := d.fetchLegacyDetail(ctx, sourceID)
	return drama.DisplayTitle(), chapters, err
}

func (d *Downloader) fetchLegacyDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	var detail detailResponse
	if err := d.fetchAPI(ctx, "/api/app/playlet/detail/"+url.PathEscape(sourceID), nil, &detail); err != nil {
		return Drama{}, nil, err
	}
	chapters := detail.Chapters
	if len(chapters) == 0 {
		if err := d.fetchAPI(ctx, "/api/app/playlet-chapter/list/"+url.PathEscape(sourceID), nil, &chapters); err != nil {
			return Drama{}, nil, err
		}
	}
	chapters = uniqueChapters(chapters)
	for index := range chapters {
		chapter := &chapters[index]
		chapter.Source = sourceCloudFront
		chapter.ID = providerChapterID(sourceCloudFront, sourceID, firstNonEmpty(chapter.ID, strconv.Itoa(index+1)))
		chapter.Referer = legacyFrontendURL + "/"
		if chapterEpisodeNumber(*chapter, 0) <= 0 {
			chapter.CurrentEpisode = rawEpisode(index + 1)
		}
		chapter.Title = firstNonEmpty(chapter.Title, "第"+chapter.EpisodeString(index+1)+"集")
	}
	sortProviderChapters(chapters)
	drama := detail.Drama
	drama.Title, drama.Name = detail.Title, detail.Name
	if drama.ID != "" && drama.ID != sourceID && drama.ID != providerDramaID(sourceCloudFront, sourceID) {
		return Drama{}, nil, errors.New("黄果旧版详情与请求剧集不符")
	}
	drama.ID, drama.Source, drama.SourceID = providerDramaID(sourceCloudFront, sourceID), sourceCloudFront, sourceID
	for _, cover := range []any{drama.CoverURL, drama.CoverURLSnake, drama.Cover, drama.ImageURL, drama.ImageURLSnake, drama.Image, drama.Img, drama.Pic, drama.Poster} {
		if address := legacyCoverURL(cover); address != "" {
			drama.Cover, drama.CoverURL = address, address
			break
		}
	}
	return drama, chapters, nil
}

func (d *Downloader) resolveLegacyMedia(ctx context.Context, task Task) (providerMedia, error) {
	if task.Chapter.VideoURL == "" {
		return providerMedia{}, errors.New("此分集没有可用的播放地址")
	}
	var key []byte
	if d.cfg.AESKeyHex != "" {
		var err error
		key, err = hex.DecodeString(d.cfg.AESKeyHex)
		if err != nil || len(key) != 16 {
			return providerMedia{}, errors.New("aesKeyHex 必须是 16 字节 AES 密钥的 hex 编码")
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		access, err := d.legacyCredentials(ctx)
		if err != nil {
			return providerMedia{}, err
		}
		base, err := d.apiEndpoint(ctx)
		if err != nil {
			return providerMedia{}, err
		}
		address := task.Chapter.VideoURL
		if !isProviderHTTPMediaURL(address) {
			address = base + "/api/app/vid/h5/m3u8/" + url.PathEscape(strings.Trim(address, "/")) + "?" + url.Values{
				"token": {access.Token}, "c": {firstNonEmpty(d.cfg.CDNURL, defaultCDNURL)},
			}.Encode()
		}
		playlist, finalURL, err := d.fetchMediaPlaylist(ctx, address, legacyFrontendURL+"/")
		if err != nil {
			var expired *legacyAPIError
			if attempt == 0 && access.Anonymous && errors.As(err, &expired) && expired.code == "5005" {
				d.invalidateLegacyToken(access)
				continue
			}
			return providerMedia{}, err
		}
		return providerMedia{URL: finalURL, Playlist: playlist, Referer: legacyFrontendURL + "/", HLSKey: key, Duration: m3u8Duration(playlist)}, nil
	}
	return providerMedia{}, errors.New("黄果旧 API 的播放会话已失效，请稍后重试")
}
