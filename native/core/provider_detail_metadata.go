package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func parseHongguoSortDetail(body, id string) (Drama, error) {
	row := nestedMap(routerLoaderMap(parseRouterData(body), "detail_page", "detail_"), "seriesDetail")
	if mapString(row, "series_id", "series_id_str") != id || mapString(row, "series_name", "series_title") == "" {
		return Drama{}, errors.New("红果网页详情与请求剧集不符")
	}
	patch := hongguoDramaFromAny(row, "")
	patch.Title = mapString(row, "series_name", "series_title")
	patch.Name = patch.Title
	patch.OnlineDate = providerTimestampDate(mapString(row, "first_visible_time"))
	if cover := hongguoCoverAddress(mapString(row, "series_cover", "cover")); cover != "" {
		patch.Cover, patch.CoverURL = cover, cover
	}
	return patch, nil
}

func parseHuangguoSortDetail(body, pageURL string, patch Drama) (Drama, error) {
	type entry struct {
		Type      string `json:"@type"`
		ID        string `json:"@id"`
		URL       string `json:"url"`
		Name      string `json:"name"`
		Published string `json:"datePublished"`
		Uploaded  string `json:"uploadDate"`
		Image     any    `json:"image"`
		Thumbnail any    `json:"thumbnailUrl"`
	}
	expected, _ := url.Parse(pageURL)
	matched := false
	for _, block := range rankingJSONLD.FindAllStringSubmatch(body, -1) {
		var graph struct {
			Graph []entry `json:"@graph"`
			entry
		}
		if json.Unmarshal([]byte(block[1]), &graph) != nil {
			continue
		}
		for _, row := range append(graph.Graph, graph.entry) {
			if row.Type != "WebPage" && row.Type != "VideoObject" && row.Type != "TVSeries" && row.Type != "Movie" {
				continue
			}
			actual, err := url.Parse(firstNonEmpty(row.URL, row.ID))
			if err != nil || actual.Host != "" && !strings.EqualFold(actual.Host, expected.Host) && providerSourceForURL(actual.String()) != patch.Source || strings.TrimRight(actual.Path, "/") != strings.TrimRight(expected.Path, "/") || row.Name == "" || patch.Source == sourceHuangguoAI && huangguoTitleNeedsRepair(row.Name, patch.SourceID) {
				continue
			}
			matched = true
			patch.Title = row.Name
			if address := firstNonEmpty(providerCoverAddress(row.Image, pageURL), providerCoverAddress(row.Thumbnail, pageURL)); address != "" {
				patch.Cover, patch.CoverURL = address, address
			}
			if date := providerReleaseDate(firstNonEmpty(row.Uploaded, row.Published)); date != "" && (patch.OnlineDate == "" || row.Type == "VideoObject") {
				patch.OnlineDate = date
			}
		}
	}
	if !matched {
		return patch, fmt.Errorf("黄果详情没有返回所请求剧集的元数据")
	}
	if nativeNormalize(patch).Cover == "" {
		for _, tag := range coverMetaTag.FindAllString(body, -1) {
			if strings.ToLower(extractAttr(tag, "property", "name")) == "og:image" {
				if address := providerCoverAddress(extractAttr(tag, "content"), pageURL); address != "" {
					patch.Cover, patch.CoverURL = address, address
					break
				}
			}
		}
	}
	markup := huangguoNonContent.ReplaceAllString(body, "")
	metaBlock := huangguoClassBlock(markup, "hg-web-detail__meta")
	meta := cleanText(metaBlock)
	patch.Views = normalizeViews(firstMatchText(reViewsText, meta))
	if patch.OnlineDate == "" && strings.Contains(meta, "上线") {
		patch.OnlineDate = providerReleaseDate(firstMatchText(reDateText, meta))
	}
	if patch.Source == sourceHuangguoAI {
		episode := firstNonEmpty(extractAttr(metaBlock, "data-ep-base"), meta)
		if count := episodeIndex(episode, 0); count > 0 {
			patch.TotalEpisode, patch.EpisodeCount = count, count
		}
	}
	return patch, nil
}

func huangguoDetailMetadata(body, pageURL, source, sourceID string) Drama {
	drama := Drama{ID: providerDramaID(source, sourceID), Source: source, SourceID: sourceID,
		Title: firstNonEmpty(extractPageTitle(body), "短剧"), Desc: extractDescription(body), Tags: extractTags(body)}
	if patch, err := parseHuangguoSortDetail(body, pageURL, drama); err == nil {
		drama = patch
	}
	drama.Name, drama.Intro = drama.Title, drama.Desc
	meta := cleanText(huangguoClassBlock(huangguoNonContent.ReplaceAllString(body, ""), "hg-web-detail__meta"))
	drama.ReleaseStatus = releaseStatusFromRemark(meta)
	return drama
}
