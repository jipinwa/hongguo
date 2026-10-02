package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var coverMetaTag = regexp.MustCompile(`(?is)<meta\b[^>]*>`)

func (d *Downloader) nativeCoverAddress(ctx context.Context, drama nativeDrama) (string, error) {
	source, id, valid := splitProviderDramaID(drama.ID)
	if !valid {
		return "", errors.New("无效的剧集 ID")
	}
	switch source {
	case sourceHongguo:
		if !hongguoNumericID.MatchString(id) {
			return "", errors.New("无效的红果剧集 ID")
		}
		result, err := d.hongguoAppRequest(ctx, http.MethodPost, "/novel/player/video_detail/v1/", nil, map[string]any{"series_id": id})
		if err != nil {
			return "", err
		}
		row := nestedMap(result, "data", "video_data")
		if mapString(row, "series_id_str", "series_id") != id {
			return "", errors.New("红果详情与请求剧集不符")
		}
		return hongguoCoverAddress(mapString(row, "series_cover", "cover")), nil
	case sourceHuangdou:
		row, err := d.huangdouDetail(ctx, id)
		if err != nil {
			return "", err
		}
		fresh := nativeNormalize(huangdouDramaFromMap(row))
		if fresh.ID != drama.ID {
			return "", errors.New("黄豆详情与请求剧集不符")
		}
		return fresh.Cover, nil
	case sourceHuangju:
		fresh, _, err := d.fetchHuangjuDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	case sourceYeguo:
		fresh, _, err := d.fetchYeguoDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	case sourceDSD:
		fresh, _, err := d.fetchDSDDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	case sourceSorani:
		fresh, _, err := d.fetchSoraniDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	case sourceGuipian:
		fresh, _, err := d.fetchGuipianDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	case sourceHanxiaoquan:
		fresh, _, err := d.fetchHanxiaoquanDetail(ctx, id)
		return nativeNormalize(fresh).Cover, err
	default:
		if isDuanjuProviderSource(source) {
			fresh, _, err := d.fetchDuanjuDetail(ctx, source, id)
			if err != nil {
				return "", err
			}
			return nativeNormalize(fresh).Cover, nil
		}
	case sourceCloudFront:
		if !rankingSourceID.MatchString(id) {
			return "", errors.New("无效的黄果剧集 ID")
		}
		var raw Drama
		if err := d.fetchAPI(ctx, "/api/app/playlet/detail/"+url.PathEscape(id), nil, &raw); err != nil {
			return "", err
		}
		if raw.ID != "" && raw.ID != id && raw.ID != drama.ID {
			return "", errors.New("黄果详情与请求剧集不符")
		}
		for _, value := range []any{raw.Cover, raw.CoverURL, raw.CoverURLSnake, raw.Image, raw.ImageURL, raw.ImageURLSnake, raw.Img, raw.Pic, raw.Picture, raw.Poster, raw.Thumb, raw.Thumbnail} {
			if address := legacyCoverURL(value); address != "" {
				return address, nil
			}
		}
		return "", errors.New("详情暂未返回海报地址")
	case sourceHuangguoAI, sourceHuangguoVideo:
		route := "/detail/" + url.PathEscape(id) + "/"
		if source == sourceHuangguoAI && !rankingSourceID.MatchString(id) {
			return "", errors.New("无效的黄果剧集 ID")
		}
		if source == sourceHuangguoVideo {
			parts := strings.Split(id, "/")
			if len(parts) == 1 {
				parts = []string{"series", id}
			}
			if len(parts) != 2 || (parts[0] != "series" && parts[0] != "video") || !rankingSourceID.MatchString(parts[1]) {
				return "", errors.New("无效的黄果剧集 ID")
			}
			route = "/" + parts[0] + "/" + url.PathEscape(parts[1])
		}
		pageURL := d.providerBaseURL(source) + route
		body, err := d.fetchProviderText(ctx, pageURL, d.providerBaseURL(source)+"/")
		if err != nil {
			return "", err
		}
		fresh, err := parseHuangguoSortDetail(body, pageURL, Drama{ID: drama.ID, Source: source, SourceID: id})
		return nativeNormalize(fresh).Cover, err
	}
	return "", errors.New("该站源暂无封面补齐接口")
}

func providerCoverAddress(value any, pageURL string) string {
	switch item := value.(type) {
	case []any:
		for _, value := range item {
			if address := providerCoverAddress(value, pageURL); address != "" {
				return address
			}
		}
	case map[string]any:
		for _, key := range []string{"url", "contentUrl", "thumbnailUrl"} {
			if address := providerCoverAddress(item[key], pageURL); address != "" {
				return address
			}
		}
	case string:
		if strings.TrimSpace(item) == "" {
			return ""
		}
		address, err := url.Parse(strings.TrimSpace(item))
		if err != nil {
			return ""
		}
		if base, err := url.Parse(pageURL); err == nil && base.IsAbs() {
			address = base.ResolveReference(address)
		}
		if validNativeCoverURL(address) {
			return address.String()
		}
	}
	return ""
}

func hongguoCoverAddress(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 8192 {
			continue
		}
		if strings.HasPrefix(value, "//") {
			value = "https:" + value
		}
		parsed, err := url.Parse(value)
		if err == nil && validNativeCoverURL(parsed) {
			return value
		}
	}
	return ""
}
