package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (d *Downloader) fetchDuanjuCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	source = canonicalProviderSource(source)
	if !isDuanjuProviderSource(source) {
		return nil, errors.New("该站源不支持分类读取")
	}
	if static, found := duanjuStaticCategories[source]; found {
		categories := make([]nativeCategory, 0, len(static))
		for _, entry := range static {
			categories = append(categories, nativeCategory{ID: entry.ID, Name: entry.Name})
		}
		return categories, nil
	}
	switch source {
	case sourceYaguo:
		return d.fetchYaguoCategories(ctx)
	case sourceMaoguo:
		return d.fetchMaoguoCategories(ctx)
	}
	return nil, errors.New("该站源暂未提供分类")
}

func (d *Downloader) fetchDuanjuCatalogPage(ctx context.Context, source string, page int, category string) ([]Drama, bool, error) {
	source = canonicalProviderSource(source)
	switch source {
	case sourceYaguo:
		return d.fetchYaguoCatalogPage(ctx, page, category)
	case sourceMaoguo:
		return d.fetchMaoguoCatalogPage(ctx, page, category)
	case sourceFanguo:
		return d.fetchFanguoCatalogPage(ctx, page, category)
	case sourceGuanguo:
		return d.fetchGuanguoCatalogPage(ctx, page, category)
	case sourceHeguo:
		return d.fetchHeguoCatalogPage(ctx, page, category)
	case sourceXingguo:
		return d.fetchXingguoCatalogPage(ctx, page, category)
	case sourceNiuguo:
		return d.fetchNiuguoCatalogPage(ctx, page, category)
	case sourceHuaguo, sourceFaguo, sourceWuguo, sourceWangguo, sourcePiguo:
		return d.fetchMaccmsCatalogPage(ctx, source, page, category)
	}
	return nil, false, errors.New("该站源暂未接入目录")
}

func (d *Downloader) fetchDuanjuDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	source = canonicalProviderSource(source)
	switch source {
	case sourceYaguo:
		return d.fetchYaguoDetail(ctx, sourceID)
	case sourceMaoguo:
		return d.fetchMaoguoDetail(ctx, sourceID)
	case sourceFanguo:
		return d.fetchFanguoDetail(ctx, sourceID)
	case sourceGuanguo:
		return d.fetchGuanguoDetail(ctx, sourceID)
	case sourceHeguo:
		return d.fetchHeguoDetail(ctx, sourceID)
	case sourceXingguo:
		return d.fetchXingguoDetail(ctx, sourceID)
	case sourceNiuguo:
		return d.fetchNiuguoDetail(ctx, sourceID)
	case sourceHuaguo, sourceFaguo, sourceWuguo, sourceWangguo, sourcePiguo:
		return d.fetchMaccmsDetail(ctx, source, sourceID)
	}
	return Drama{}, nil, errors.New("该站源暂未接入详情")
}

func (d *Downloader) searchDuanju(ctx context.Context, source, query string) ([]Drama, error) {
	source = canonicalProviderSource(source)
	switch source {
	case sourceYaguo:
		return d.searchYaguo(ctx, query)
	case sourceMaoguo:
		return d.searchMaoguo(ctx, query)
	case sourceFanguo:
		return d.searchFanguo(ctx, query)
	case sourceGuanguo:
		return d.searchGuanguo(ctx, query)
	case sourceHeguo:
		return d.searchHeguo(ctx, query)
	case sourceXingguo:
		return d.searchXingguo(ctx, query)
	case sourceNiuguo:
		return d.searchNiuguo(ctx, query)
	case sourceHuaguo, sourceFaguo, sourceWuguo, sourceWangguo, sourcePiguo:
		return d.searchMaccms(ctx, source, query)
	}
	return nil, errors.New("该站源不支持在线搜索")
}

func duanjuSupportsSearch(source string) bool {
	spec, found := duanjuSourceSpecFor(source)
	return found && spec.Searcher
}

func duanjuSupportsPaging(source string) bool {
	spec, found := duanjuSourceSpecFor(source)
	return found && spec.Paged
}

func (d *Downloader) resolveDuanjuMedia(ctx context.Context, task Task) (providerMedia, error) {
	source := canonicalProviderSource(task.Chapter.Source)
	name := duanjuSourceName(source)
	switch source {
	case sourceHeguo:
		return d.resolveHeguoMedia(ctx, task)
	case sourceNiuguo:
		return d.resolveNiuguoMedia(ctx, task)
	}
	base := d.duanjuBaseURL(source)
	if base == "" {
		return providerMedia{}, errors.New("站源地址不可用")
	}
	address := strings.TrimSpace(task.Chapter.VideoURL)
	pageURL := strings.TrimSpace(task.Chapter.PageURL)
	referer := firstNonEmpty(task.Chapter.Referer, base+"/")
	// 网页型站源的分集保存在详情页，需要即时解析真实播放地址。
	if !isProviderHTTPMediaURL(address) || !duanjuLooksLikeMedia(address) {
		if pageURL == "" {
			if source, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
				if _, chapters, err := d.fetchDuanjuDetail(ctx, source, sourceID); err == nil {
					for _, fresh := range chapters {
						if fresh.ID == task.Chapter.ID {
							address = fresh.VideoURL
							pageURL = fresh.PageURL
							break
						}
					}
				}
			}
		}
		if pageURL != "" {
			resolved, err := d.resolveDuanjuWebPage(ctx, source, pageURL, referer)
			if err != nil {
				return providerMedia{}, err
			}
			return d.prepareWebProviderMedia(ctx, resolved, name)
		}
	}
	if !isProviderHTTPMediaURL(address) {
		return providerMedia{}, fmt.Errorf("%s未返回有效播放地址，请刷新章节后重试", name)
	}
	return d.prepareWebProviderMedia(ctx, providerMedia{URL: address, Referer: referer}, name)
}

func duanjuLooksLikeMedia(address string) bool {
	lower := strings.ToLower(address)
	return strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".flv")
}

func (d *Downloader) resolveDuanjuWebPage(ctx context.Context, source, pageURL, referer string) (providerMedia, error) {
	body, err := d.fetchProviderText(ctx, pageURL, referer)
	if err != nil {
		return providerMedia{}, err
	}
	address := maccmsNormalizePlaybackURL(maccmsPlayerURL(body))
	if address == "" {
		return providerMedia{}, fmt.Errorf("%s未返回有效播放地址，请刷新章节后重试", duanjuSourceName(source))
	}
	return providerMedia{URL: address, Referer: pageURL}, nil
}

func (d *Downloader) duanjuSourceProbe(ctx context.Context, source string) (string, error) {
	source = canonicalProviderSource(source)
	items, _, err := d.fetchDuanjuCatalogPage(ctx, source, 1, "")
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("站源入口可达，但没有解析到有效剧集")
	}
	return fmt.Sprintf("已解析 %d 部剧", len(items)), nil
}

func duanjuFirstCategory(source string) string {
	if static, found := duanjuStaticCategories[canonicalProviderSource(source)]; found && len(static) > 0 {
		return static[0].ID
	}
	return ""
}
