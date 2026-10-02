package core

import (
	"context"
	"crypto/sha256"

	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"

	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	sourceHuangguoAI    = "huangguoai"
	sourceHuangguoVideo = "huangguo-video"
	sourceHuangdou      = "huangdou"
	sourceHongguo       = "hongguo"
	sourceHuangju       = "huangju"
	sourceYeguo         = "yeguo"
	sourceDSD           = "dsd"
	sourceCloudFront    = "cloudfront"
	sourceSorani        = "sorani"
	sourceGuipian       = "guipian"
	sourceHanxiaoquan   = "hanxiaoquan"

	providerMaxBodyBytes = 20 * 1024 * 1024
	providerTimeout      = 12 * time.Second
)

var (
	huangguoAIBaseURL     = "https://huangguoai.com"
	huangguoAIBaseMirrors = []string{"https://huangguoai.com", "https://ttvoij.ediayikma.cc", "https://thu.ediayikma.cc", "https://pku.ediayikma.cc", "https://fdu.ediayikma.cc", "https://thu.agdkczeyx.cc"}
	huangguoVideoBaseURL  = "https://huangguo.video"

	huangguoAISlugs     = []string{"recommend", "newest", "ai-duanju", "ai-manju", "ai-huanlian", "ai-mogai", "ranks/hot"}
	huangguoAITypeNames = map[string]string{"recommend": "精选推荐", "newest": "最近上新", "ai-duanju": "AI成人短剧", "ai-manju": "AI成人漫剧", "ai-huanlian": "AI换脸", "ai-mogai": "AI魔改", "ranks/hot": "排行榜"}

	reDetailHref      = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*/detail/([^"'/?#]+)[^"']*)["'][^>]*>.*?</a>`)
	reDramaCardStart  = regexp.MustCompile(`(?is)<div\b[^>]*class=["'][^"']*\bhg-drama-card\b[^"']*["'][^>]*>`)
	reVideoCard       = regexp.MustCompile(`(?is)<article\b[^>]*class=["'][^"']*\bvideo-card\b[^"']*["'][^>]*>.*?</article>`)
	reAnyArticle      = regexp.MustCompile(`(?is)<article\b[^>]*>.*?</article>`)
	reHuangguoLink    = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*/(?:series|video)/([^"'/?#]+)[^"']*)["'][^>]*>.*?</a>`)
	reAIEpisodeLink   = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*/video/[^"']+)["'][^>]*>.*?</a>`)
	reTitleTag        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reH1Tag           = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	reDescMeta        = regexp.MustCompile(`(?is)<meta\b[^>]*(?:name|property)=["'](?:description|og:description)["'][^>]*content=["']([^"']+)["'][^>]*>`)
	reEpisodeNumber   = regexp.MustCompile(`(?i)(?:第\s*0*(\d+)\s*(?:集|话|期)|(?:更新至|共|全)\s*0*(\d+)\s*(?:集|话|期)|(?:episode|ep)\s*#?\s*0*(\d+))`)
	reResolution      = regexp.MustCompile(`(?i)RESOLUTION\s*=\s*(\d+)x(\d+)`)
	reBandwidth       = regexp.MustCompile(`(?i)(?:AVERAGE-)?BANDWIDTH\s*=\s*(\d+)`)
	reDataHLS         = regexp.MustCompile(`(?is)data-hls=["']([^"']+)["']`)
	reDataPlaySrc     = regexp.MustCompile(`(?is)data-play-src=["']([^"']+)["']`)
	reMediaFieldValue = regexp.MustCompile(`(?is)["']?(?:videoSrc|videoUrl|playUrl|src|url)["']?\s*[:=]\s*["']([^"']+\.(?:m3u8|mp4)(?:[^"']*)?)["']`)
	reDateText        = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}`)
	reViewsText       = regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?\s*[w万]?\s*次播放`)
	reScoreText       = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?\s*分`)
)

type providerEpisode struct {
	Key   string
	Title string
	URL   string
	Index int
	HLS   string
}

func providerDramaID(source, sourceID string) string {
	return source + ":" + strings.TrimSpace(sourceID)
}

func providerChapterID(source, sourceID, chapterKey string) string {
	return source + ":" + strings.TrimSpace(sourceID) + ":" + strings.TrimSpace(chapterKey)
}

func splitProviderDramaID(id string) (source, sourceID string, ok bool) {
	source, sourceID, ok = strings.Cut(strings.TrimSpace(id), ":")
	source = canonicalProviderSource(source)
	if !ok || strings.TrimSpace(sourceID) == "" || !isHuangguoProviderSource(source) {
		return "", "", false
	}
	return source, strings.TrimSpace(sourceID), true
}

func isHuangguoProviderSource(source string) bool {
	switch canonicalProviderSource(source) {
	case sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou, sourceHongguo, sourceHuangju, sourceYeguo, sourceDSD, sourceCloudFront, sourceSorani, sourceGuipian, sourceHanxiaoquan:
		return true
	default:
		return isDuanjuProviderSource(source)
	}
}

func canonicalProviderSource(source string) string {
	key := strings.ToLower(strings.TrimSpace(source))
	switch key {
	case "huangguo", "huangguoai", "huangguoai.com":
		return sourceHuangguoAI
	case "huangguo-video", "huangguo.video":
		return sourceHuangguoVideo
	case "huangdou", "tideember.cc", "xqjurgek.top":
		return sourceHuangdou
	case "hongguo", "hongguoduanju.com":
		return sourceHongguo
	case "huangju", "huangju.net", "api.huangju.net":
		return sourceHuangju
	case "yeguo", "ygdj7.com", "www.ygdj7.com", "analyze.buxefaex.cc", "delta.ygrwdsgt.cc", "yeguodj.com", "www.yeguodj.com":
		return sourceYeguo
	case "dsd", "dsd.com.se", "www.dsd.com.se":
		return sourceDSD
	case "cloudfront":
		return sourceCloudFront
	case "sorani", "sorani.net", "www.sorani.net", "api.sorani.cc", "sorani.cc":
		return sourceSorani
	case "guipian", "guipianwu.com", "www.guipianwu.com":
		return sourceGuipian
	case "hanxiaoquan", "jennyhow.com", "www.jennyhow.com":
		return sourceHanxiaoquan
	}
	if canonical, found := duanjuSourceAliases[key]; found {
		return canonical
	}
	return strings.TrimSpace(source)
}

func mergeDramaMetadata(base, extra Drama) Drama {
	if base.VIP == nil && extra.VIP != nil {
		value := *extra.VIP
		base.VIP = &value
	}
	if extra.SortMetadata != nil && (base.SortMetadata == nil || extra.SortMetadata.CheckedAt.After(base.SortMetadata.CheckedAt)) {
		base.SortMetadata = extra.SortMetadata
	}
	if strings.TrimSpace(base.ID) == "" {
		base.ID = extra.ID
	}
	if strings.TrimSpace(base.Source) == "" {
		base.Source = extra.Source
	}
	if strings.TrimSpace(base.SourceID) == "" {
		base.SourceID = extra.SourceID
	}
	if strings.TrimSpace(base.Title) == "" {
		base.Title = extra.Title
	}
	if strings.TrimSpace(base.Name) == "" {
		base.Name = extra.Name
	}
	if strings.TrimSpace(base.Desc) == "" {
		base.Desc = extra.Desc
	}
	if strings.TrimSpace(base.Intro) == "" {
		base.Intro = extra.Intro
	}
	if coverPathFromAny(base.Cover) == "" {
		base.Cover = extra.Cover
	}
	if coverPathFromAny(base.CoverURL) == "" {
		base.CoverURL = extra.CoverURL
	}
	if valueEmpty(base.TotalEpisode) {
		base.TotalEpisode = extra.TotalEpisode
	}
	if valueEmpty(base.EpisodeCount) {
		base.EpisodeCount = extra.EpisodeCount
	}
	if strings.TrimSpace(base.ChannelName) == "" {
		base.ChannelName = extra.ChannelName
	}
	if strings.TrimSpace(base.CategoryName) == "" || (base.CategoryName == "首页" && extra.CategoryName != "") {
		base.CategoryName = extra.CategoryName
	}
	if strings.TrimSpace(base.Remark) == "" || base.Remark == "在线观看" {
		base.Remark = extra.Remark
	}
	if strings.TrimSpace(base.Score) == "" {
		base.Score = extra.Score
	}
	if strings.TrimSpace(base.Views) == "" {
		base.Views = extra.Views
	}
	if strings.TrimSpace(base.Heat) == "" {
		base.Heat = extra.Heat
	}
	if strings.TrimSpace(base.OnlineDate) == "" {
		base.OnlineDate = extra.OnlineDate
	}
	if len(base.Tags) == 0 {
		base.Tags = extra.Tags
	}
	if base.ReleaseStatus == "" || base.ReleaseStatus == "unknown" {
		base.ReleaseStatus = extra.ReleaseStatus
	}
	if dramaProvider(base) == sourceHuangguoAI && base.ID == extra.ID {
		_, sourceID, _ := splitProviderDramaID(base.ID)
		if title := firstHuangguoTitle(sourceID, base.Title, base.Name, extra.Title, extra.Name); title != "" {
			base.Title, base.Name = title, title
		}
	}
	return base
}

func valueEmpty(v any) bool {
	if v == nil {
		return true
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	return s == "" || s == "0" || s == "<nil>"
}

func (d *Downloader) GetHuangguoChapters(ctx context.Context, source, sourceID string) (string, []Chapter, error) {
	switch canonicalProviderSource(source) {
	case sourceHuangguoAI:
		return d.fetchHuangguoAIChapters(ctx, sourceID)
	case sourceHuangguoVideo:
		return d.fetchHuangguoVideoChapters(ctx, sourceID)
	case sourceHuangdou:
		return d.fetchHuangdouChapters(ctx, sourceID)
	case sourceHongguo:
		return d.fetchHongguoChapters(ctx, sourceID)
	case sourceHuangju:
		drama, chapters, err := d.fetchHuangjuDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceYeguo:
		drama, chapters, err := d.fetchYeguoDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceDSD:
		drama, chapters, err := d.fetchDSDDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceSorani:
		drama, chapters, err := d.fetchSoraniDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceGuipian:
		drama, chapters, err := d.fetchGuipianDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceHanxiaoquan:
		drama, chapters, err := d.fetchHanxiaoquanDetail(ctx, sourceID)
		return drama.DisplayTitle(), chapters, err
	case sourceCloudFront:
		return d.fetchLegacyChapters(ctx, sourceID)
	default:
		if isDuanjuProviderSource(source) {
			drama, chapters, err := d.fetchDuanjuDetail(ctx, source, sourceID)
			return drama.DisplayTitle(), chapters, err
		}
		return "", nil, fmt.Errorf("unsupported provider source: %s", source)
	}
}

func (d *Downloader) fetchHuangguoAIChapters(ctx context.Context, sourceID string) (string, []Chapter, error) {
	drama, chapters, err := d.fetchHuangguoAIDetail(ctx, sourceID)
	return drama.DisplayTitle(), chapters, err
}

func (d *Downloader) fetchHuangguoAIDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.Trim(strings.TrimSpace(sourceID), "/")
	sourceID = strings.TrimPrefix(sourceID, "detail/")
	if sourceID == "" {
		return Drama{}, nil, fmt.Errorf("empty huangguoai sourceID")
	}
	detailURL := strings.TrimRight(huangguoAIBaseURL, "/") + "/detail/" + url.PathEscape(sourceID) + "/"
	body, err := d.fetchProviderText(ctx, detailURL, huangguoAIBaseURL+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	drama := huangguoDetailMetadata(body, detailURL, sourceHuangguoAI, sourceID)
	title := drama.DisplayTitle()
	episodes := parseHuangguoAIEpisodes(body, detailURL, sourceID)
	if len(episodes) == 0 {
		if media := parseAIVideoURL(body, detailURL); media != "" {
			episodes = []providerEpisode{{Key: "1", Title: title, URL: detailURL, Index: 1, HLS: media}}
		}
	}
	if len(episodes) == 0 {
		return drama, nil, nil
	}
	var chapters []Chapter
	for _, ep := range episodes {
		mediaURL := ep.HLS
		idx := ep.Index
		if idx <= 0 {
			idx = len(chapters) + 1
		}
		chapterTitle := strings.TrimSpace(ep.Title)
		if chapterTitle == "" {
			chapterTitle = fmt.Sprintf("第%d集", idx)
		}
		key := ep.Key
		if key == "" {
			key = strconv.Itoa(idx)
		}
		chapters = append(chapters, Chapter{ID: providerChapterID(sourceHuangguoAI, sourceID, key), Source: sourceHuangguoAI, Title: chapterTitle, VideoURL: mediaURL, PageURL: ep.URL, CurrentEpisode: rawEpisode(idx)})
	}
	sortProviderChapters(chapters)
	return drama, uniqueChapters(chapters), nil
}

func (d *Downloader) fetchHuangguoVideoChapters(ctx context.Context, sourceID string) (string, []Chapter, error) {
	drama, chapters, err := d.fetchHuangguoVideoDetail(ctx, sourceID)
	return drama.DisplayTitle(), chapters, err
}

func (d *Downloader) fetchHuangguoVideoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.Trim(strings.TrimSpace(sourceID), "/")
	if sourceID == "" {
		return Drama{}, nil, fmt.Errorf("empty huangguo-video sourceID")
	}
	detailPath := sourceID
	if !strings.HasPrefix(detailPath, "series/") && !strings.HasPrefix(detailPath, "video/") {
		detailPath = "series/" + detailPath
	}
	detailURL := strings.TrimRight(huangguoVideoBaseURL, "/") + "/" + detailPath
	body, err := d.fetchProviderText(ctx, detailURL, huangguoVideoBaseURL+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	drama := huangguoDetailMetadata(body, detailURL, sourceHuangguoVideo, sourceID)
	title := drama.DisplayTitle()
	var episodes []providerEpisode
	if strings.HasPrefix(detailPath, "video/") {
		if hls := parseDataHLS(body, detailURL); hls != "" {
			episodes = []providerEpisode{{Key: "1", Title: title, URL: detailURL, Index: 1, HLS: hls}}
		}
	} else {
		episodes = parseHuangguoVideoEpisodes(body, detailURL)
	}
	if len(episodes) == 0 {
		if hls := parseDataHLS(body, detailURL); hls != "" {
			episodes = []providerEpisode{{Key: "1", Title: title, URL: detailURL, Index: 1, HLS: hls}}
		}
	}
	var chapters []Chapter
	for _, ep := range episodes {
		hlsURL := ep.HLS
		idx := ep.Index
		if idx <= 0 {
			idx = len(chapters) + 1
		}
		chapterTitle := strings.TrimSpace(ep.Title)
		if chapterTitle == "" {
			chapterTitle = fmt.Sprintf("第%d集", idx)
		}
		key := ep.Key
		if key == "" {
			key = strconv.Itoa(idx)
		}
		chapters = append(chapters, Chapter{ID: providerChapterID(sourceHuangguoVideo, sourceID, key), Source: sourceHuangguoVideo, Title: chapterTitle, VideoURL: hlsURL, PageURL: ep.URL, CurrentEpisode: rawEpisode(idx)})
	}
	sortProviderChapters(chapters)
	return drama, uniqueChapters(chapters), nil
}

func (d *Downloader) fetchProviderText(ctx context.Context, rawURL, referer string) (string, error) {
	retries := d.cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	if referer == "" {
		referer = rawURL
	}
	candidates := d.providerURLCandidates(rawURL)
	var lastErr error
	tried := 0
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		attempts := retries
		if len(candidates) > 1 {
			attempts = 1
		}
		for attempt := 1; attempt <= attempts; attempt++ {
			tried++
			if attempt > 1 {
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			timeout := providerTimeout
			if len(candidates) > 1 {
				timeout = 5 * time.Second
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", userAgent)
			if agent, ok := ctx.Value(providerTextUserAgentKey{}).(string); ok && agent != "" {
				req.Header.Set("User-Agent", agent)
			}
			if providerSourceForURL(rawURL) != "" {
				req.Header.Set("Referer", providerRefererForURL(candidate, referer))
			} else {
				req.Header.Set("Referer", referer)
			}
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
			if noCache, _ := ctx.Value(providerTextNoCacheKey{}).(bool); noCache {
				req.Header.Set("Cache-Control", "no-cache")
			}
			resp, err := d.doCatalogRequestWithTimeout(req, timeout)
			if err != nil {
				lastErr = err
				var backoff *requestBackoff
				if errors.As(err, &backoff) {
					break
				}
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
			closeErr := resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}
			if closeErr != nil {
				lastErr = closeErr
				continue
			}
			if len(body) > providerMaxBodyBytes {
				lastErr = fmt.Errorf("response exceeds %d bytes", providerMaxBodyBytes)
				continue
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 || catalogResponseBlockReason(resp, body) != "" {
				lastErr = d.catalogResponseError(req, resp, body)
				if catalogResponseBlockReason(resp, body) != "" || resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 {
					break
				}
				continue
			}
			if source := providerSourceForURL(rawURL); source != "" {
				effectiveURL := req.URL
				if resp.Request != nil {
					effectiveURL = resp.Request.URL
				}
				d.providerMu.Lock()
				d.providerHosts[source] = effectiveURL.Scheme + "://" + effectiveURL.Host
				d.providerMu.Unlock()
			}
			rememberPlaybackResponseURL(ctx, rawURL, resp)
			return string(body), nil
		}
	}
	if len(candidates) > 1 && lastErr != nil {
		return "", fmt.Errorf("黄果来源请求失败：已尝试 %d 个域名，最后错误：%w", tried, lastErr)
	}
	return "", lastErr
}

func providerURLCandidates(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() != "huangguoai.com" {
		return []string{rawURL}
	}
	out := make([]string, 0, len(huangguoAIBaseMirrors)+8)
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	for i, mirror := range huangguoAIBaseMirrors {
		if i == 1 {
			for _, generated := range generatedHuangguoAIMirrors(rawURL, 8) {
				add(rehostProviderURL(u, generated))
			}
		}
		add(rehostProviderURL(u, mirror))
	}
	if len(out) == 0 {
		return []string{rawURL}
	}
	return out
}

func rehostProviderURL(u *url.URL, mirror string) string {
	mu, err := url.Parse(mirror)
	if err != nil || mu.Host == "" {
		return ""
	}
	copyURL := *u
	copyURL.Scheme = mu.Scheme
	copyURL.Host = mu.Host
	return copyURL.String()
}

func generatedHuangguoAIMirrors(seed string, n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, 0, n)
	seen := map[string]bool{}
	for i := 0; len(out) < n && i < n*4; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", seed, i)))
		hexText := fmt.Sprintf("%x", sum[:])
		sub := strings.TrimLeft(hexText[:6], "0123456789")
		if len(sub) < 4 {
			sub = hexText[6:12]
		}
		if len(sub) > 6 {
			sub = sub[:6]
		}
		if len(sub) < 3 || seen[sub] {
			continue
		}
		seen[sub] = true
		out = append(out, "https://"+sub+".ediayikma.cc")
	}
	return out
}

func providerRefererForURL(candidate, fallback string) string {
	target, err := url.Parse(candidate)
	previous, previousErr := url.Parse(fallback)
	if err != nil || target.Host == "" || previousErr != nil || previous.Host == "" {
		return fallback
	}
	if source := providerSourceForURL(fallback); source != "" && (source == providerSourceForURL(candidate) || providerSourceForURL(candidate) == "") {
		previous.Scheme, previous.Host = target.Scheme, target.Host
	}
	previous.Fragment = ""
	return previous.String()
}

func parseHuangguoAIDramaCards(rawHTML, pageURL, category string) []Drama {
	positions := map[string]int{}
	var out []Drama
	add := func(dr Drama) {
		if dr.ID == "" || dr.SourceID == "" {
			return
		}
		if position, found := positions[dr.SourceID]; found {
			out[position] = mergeDramaMetadata(out[position], dr)
			return
		}
		positions[dr.SourceID] = len(out)
		out = append(out, dr)
	}
	markup := huangguoNonContent.ReplaceAllString(rawHTML, "")
	for _, block := range splitHuangguoAICardBlocks(markup) {
		sourceID := cleanID(firstNonEmpty(extractAttr(block, "data-track-id"), detailIDFromString(extractAttr(block, "href"))))
		if sourceID == "" {
			if m := reDetailHref.FindStringSubmatch(block); len(m) > 2 {
				sourceID = cleanID(m[2])
			}
		}
		if sourceID == "" {
			continue
		}
		title := huangguoCardTitle(block, sourceID)
		cover := resolveProviderURL(pageURL, firstNonEmpty(extractAttr(block, "data-src"), extractAttr(block, "data-original"), extractAttr(block, "src")))
		desc := firstNonEmpty(extractByClassText(block, "hg-drama-card__desc"), extractDescription(block))
		score := firstNonEmpty(extractByClassText(block, "hg-drama-card__score"), firstMatchText(reScoreText, block))
		episodeBlock := huangguoClassBlock(block, "hg-drama-card__episode")
		episode := firstNonEmpty(extractAttr(episodeBlock, "data-ep-base"), cleanText(episodeBlock))
		badge := extractByClassText(block, "hg-drama-card__badge")
		remark := strings.TrimSpace(strings.Join(nonEmptyStrings(badge, episode), " "))
		if remark == "" {
			remark = "在线观看"
		}
		views := normalizeViews(firstNonEmpty(firstMatchText(reViewsText, block), extractByClassText(block, "hg-drama-card__views"), extractByClassText(block, "hg-drama-card__play")))
		online := normalizeDate(firstMatchText(reDateText, block))
		tags := extractTags(block)
		typeName := firstNonEmpty(extractAttr(block, "data-track-type-name"), category)
		if title == "" {
			title = sourceID
		}
		dr := Drama{ID: providerDramaID(sourceHuangguoAI, sourceID), Source: sourceHuangguoAI, SourceID: sourceID, Title: title, Name: title, Desc: desc, Intro: desc, Cover: cover, CoverURL: cover, CategoryName: typeName, ChannelName: "huangguoai.com", Remark: remark, Score: score, Views: views, OnlineDate: online, Tags: tags}
		if n := episodeIndex(episode, 0); n > 0 {
			dr.TotalEpisode = n
			dr.EpisodeCount = n
		}
		add(dr)
	}
	for _, m := range reDetailHref.FindAllStringSubmatchIndex(markup, -1) {
		if len(m) < 6 || m[4] < 0 || m[5] < 0 {
			continue
		}
		sourceID := cleanID(markup[m[4]:m[5]])
		block := markup[m[0]:m[1]]
		title := firstHuangguoTitle(sourceID, huangguoCardTitle(block, sourceID), cleanText(block))
		if title == "" {
			continue
		}
		cover := resolveProviderURL(pageURL, extractAttr(block, "data-src", "data-original", "src"))
		add(Drama{ID: providerDramaID(sourceHuangguoAI, sourceID), Source: sourceHuangguoAI, SourceID: sourceID, Title: title, Name: title, Cover: cover, CoverURL: cover, CategoryName: category, ChannelName: "huangguoai.com"})
	}
	for _, dr := range parseHuangguoAIJSONLDDramas(rawHTML, category) {
		add(dr)
	}
	return out
}

func splitHuangguoAICardBlocks(raw string) []string {
	starts := reDramaCardStart.FindAllStringIndex(raw, -1)
	blocks := make([]string, 0, len(starts))
	for i, m := range starts {
		end := len(raw)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		blocks = append(blocks, huangguoElementBlock(raw[:end], m[0], m[1], "div"))
	}
	return blocks
}

func parseHuangguoAIJSONLDDramas(raw, category string) []Drama {
	var out []Drama
	seen := map[string]bool{}
	for _, block := range regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`).FindAllStringSubmatch(raw, -1) {
		if len(block) < 2 {
			continue
		}
		var value any
		if json.Unmarshal([]byte(html.UnescapeString(block[1])), &value) != nil {
			continue
		}
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case []any:
				for _, item := range x {
					walk(item)
				}
			case map[string]any:
				name, _ := x["name"].(string)
				itemURL, _ := x["url"].(string)
				id := detailIDFromString(itemURL)
				if id != "" && !huangguoTitleNeedsRepair(name, id) && !seen[id] {
					seen[id] = true
					title := cleanText(name)
					out = append(out, Drama{ID: providerDramaID(sourceHuangguoAI, id), Source: sourceHuangguoAI, SourceID: id, Title: title, Name: title, CategoryName: category, ChannelName: "huangguoai.com"})
				}
				for _, child := range x {
					walk(child)
				}
			}
		}
		walk(value)
	}
	return out
}

func parseHuangguoAIJSONCards(raw []byte, pageURL, category string) []Drama {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Drama
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			if dr, ok := dramaFromAIMap(x, pageURL, category); ok {
				if !seen[dr.SourceID] {
					seen[dr.SourceID] = true
					out = append(out, dr)
				}
				return
			}
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(decoded)
	return out
}

func dramaFromAIMap(m map[string]any, pageURL, category string) (Drama, bool) {
	sourceID := firstNonEmpty(mapString(m, "id"), mapString(m, "videoId"), mapString(m, "video_id"), mapString(m, "vid"), mapString(m, "slug"))
	if sourceID == "" {
		for _, v := range m {
			if s, ok := v.(string); ok {
				if id := detailIDFromString(s); id != "" {
					sourceID = id
					break
				}
			}
		}
	}
	if detailID := detailIDFromString(sourceID); detailID != "" {
		sourceID = detailID
	}
	sourceID = cleanID(sourceID)
	if sourceID == "" {
		return Drama{}, false
	}
	title := firstNonEmpty(mapString(m, "title"), mapString(m, "name"), mapString(m, "videoTitle"), mapString(m, "video_title"), mapString(m, "videoName"), mapString(m, "video_name"))
	if huangguoTitleNeedsRepair(title, sourceID) {
		return Drama{}, false
	}
	cover := resolveProviderURL(pageURL, firstNonEmpty(mapString(m, "cover"), mapString(m, "coverUrl"), mapString(m, "cover_url"), mapString(m, "coverImage"), mapString(m, "cover_image"), mapString(m, "image"), mapString(m, "imageUrl"), mapString(m, "thumbnail"), mapString(m, "poster"), mapString(m, "posterUrl")))
	desc := firstNonEmpty(mapString(m, "desc"), mapString(m, "description"), mapString(m, "intro"), mapString(m, "summary"))
	eps := huangguoMapEpisodes(m)
	remark := firstNonEmpty(mapString(m, "vod_remarks"), mapString(m, "remark"), mapString(m, "remarks"))
	if remark == "" && eps != "" {
		if strings.EqualFold(mapString(m, "is_finished"), "true") || mapString(m, "is_finished") == "1" {
			remark = "全" + eps + "集"
		} else {
			remark = "更新至 " + eps + " 集"
		}
	}
	score := firstNonEmpty(mapString(m, "score"), mapString(m, "rating"))
	if score != "" && !strings.Contains(score, "分") {
		score += "分"
	}
	views := normalizeViews(firstNonEmpty(mapString(m, "views"), mapString(m, "view_count"), mapString(m, "viewCount"), mapString(m, "view_num"), mapString(m, "viewNum"), mapString(m, "views_text"), mapString(m, "view_count_text"), mapString(m, "play_count"), mapString(m, "playCount"), mapString(m, "play_num"), mapString(m, "playNum"), mapString(m, "play_count_text"), mapString(m, "hits"), mapString(m, "hit_count"), mapString(m, "plays"), mapString(m, "play")))
	online := providerReleaseDate(firstNonEmpty(mapString(m, "onlineDate"), mapString(m, "online_date"), mapString(m, "online_time"), mapString(m, "onlineTime"), mapString(m, "publish_date"), mapString(m, "publishDate"), mapString(m, "publish_time"), mapString(m, "publishTime"), mapString(m, "release_date"), mapString(m, "releaseDate")))
	tags := mapStringSlice(m, "tags", "tag", "categories", "category")
	return Drama{ID: providerDramaID(sourceHuangguoAI, sourceID), Source: sourceHuangguoAI, SourceID: sourceID, Title: title, Name: title, Desc: desc, Intro: desc, Cover: cover, CoverURL: cover, TotalEpisode: eps, EpisodeCount: eps, CategoryName: category, ChannelName: "huangguoai.com", Remark: remark, Score: score, Views: views, OnlineDate: online, Tags: tags}, true
}

func parseHuangguoVideoCards(rawHTML, pageURL string) []Drama {
	blocks := reVideoCard.FindAllString(rawHTML, -1)
	if len(blocks) == 0 {
		for _, block := range reAnyArticle.FindAllString(rawHTML, -1) {
			if strings.Contains(strings.ToLower(block), "video-card") {
				blocks = append(blocks, block)
			}
		}
	}
	seen := map[string]bool{}
	var out []Drama
	for _, block := range blocks {
		link := reHuangguoLink.FindStringSubmatch(block)
		if len(link) < 3 {
			continue
		}
		path, sourceID := huangguoVideoSourceID(link[1])
		if sourceID == "" || seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		title := firstNonEmpty(extractClosestAttr(block, 0, len(block), "title", "alt", "aria-label"), cleanText(block))
		cover := resolveProviderURL(pageURL, extractClosestAttr(block, 0, len(block), "data-src", "data-original", "src"))
		category := "video"
		if strings.HasPrefix(path, "series/") {
			category = "series"
		}
		desc := firstNonEmpty(extractClosestAttr(block, 0, len(block), "data-description", "description"), extractDescription(block))
		out = append(out, Drama{ID: providerDramaID(sourceHuangguoVideo, sourceID), Source: sourceHuangguoVideo, SourceID: sourceID, Title: title, Name: title, Desc: desc, Intro: desc, Cover: cover, CoverURL: cover, CategoryName: category, ChannelName: "huangguo.video", Remark: firstNonEmpty(extractByClassText(block, "bg-black/55"), extractByClassText(block, "text-gold-dim"))})
	}
	return out
}

func parseHuangguoAIEpisodes(rawHTML, pageURL, sourceID string) []providerEpisode {
	matches := reAIEpisodeLink.FindAllStringSubmatchIndex(rawHTML, -1)
	seen := map[string]bool{}
	var episodes []providerEpisode
	usedIndex := map[int]bool{}
	for _, m := range matches {
		if len(m) < 4 || m[2] < 0 || m[3] < 0 {
			continue
		}
		href := rawHTML[m[2]:m[3]]
		fullURL := resolveProviderURL(pageURL, href)
		linkHTML := rawHTML[m[0]:m[1]]
		title := firstNonEmpty(extractAttr(linkHTML, "title", "aria-label"), cleanText(linkHTML))
		key := aiEpisodeKey(href, sourceID)
		if key == "" {
			key = fmt.Sprintf("auto-%d", len(episodes)+1)
		}
		if seen[key] || seen[fullURL] {
			continue
		}
		seen[key] = true
		seen[fullURL] = true
		idx := episodeIndex(title, 0)
		if idx <= 0 {
			idx = len(episodes) + 1
		}
		idx = nextUnusedEpisodeIndex(idx, usedIndex)
		usedIndex[idx] = true
		episodes = append(episodes, providerEpisode{Key: key, Title: title, URL: fullURL, Index: idx})
	}
	sortProviderEpisodes(episodes)
	return episodes
}

func parseHuangguoVideoEpisodes(rawHTML, pageURL string) []providerEpisode {
	blocks := reVideoCard.FindAllString(rawHTML, -1)
	if len(blocks) == 0 {
		blocks = reHuangguoLink.FindAllString(rawHTML, -1)
	}
	positions := map[string]int{}
	var episodes []providerEpisode
	numbered := false
	for _, block := range blocks {
		link := reHuangguoLink.FindStringSubmatch(block)
		if len(link) < 2 {
			continue
		}
		path, key := huangguoVideoSourceID(link[1])
		if key == "" || !strings.HasPrefix(path, "video/") {
			continue
		}
		fullURL := resolveProviderURL(pageURL, link[1])
		title := firstNonEmpty(extractAttr(block, "title"), extractAttr(block, "alt"), cleanText(block))
		index := episodeIndex(cleanText(block), episodeIndex(title, 0))
		numbered = numbered || index > 0
		episode := providerEpisode{Key: key, Title: title, URL: fullURL, Index: index, HLS: parseDataHLS(block, pageURL)}
		if position, found := positions[fullURL]; found {
			if episodes[position].Index <= 0 && index > 0 {
				episodes[position] = episode
			}
		} else {
			positions[fullURL] = len(episodes)
			episodes = append(episodes, episode)
		}
	}
	var result []providerEpisode
	used := map[int]bool{}
	for _, episode := range episodes {
		if numbered && episode.Index <= 0 {
			continue
		}
		episode.Index = nextUnusedEpisodeIndex(episode.Index, used)
		used[episode.Index] = true
		result = append(result, episode)
	}
	sortProviderEpisodes(result)
	return result
}

func parseAIVideoURL(rawHTML, pageURL string) string {
	if media := parseAIVideoInitialData(rawHTML, pageURL); media != "" {
		return media
	}
	if m := reDataPlaySrc.FindStringSubmatch(rawHTML); len(m) > 1 {
		if media := normalizeProviderMediaURL(pageURL, m[1]); media != "" {
			return media
		}
	}
	for _, m := range reMediaFieldValue.FindAllStringSubmatch(rawHTML, -1) {
		if len(m) > 1 {
			if media := normalizeProviderMediaURL(pageURL, m[1]); media != "" {
				return media
			}
		}
	}
	return ""
}

func parseAIVideoInitialData(rawHTML, pageURL string) string {
	re := regexp.MustCompile(`(?is)<script\b[^>]*id=["']videoInitialData["'][^>]*>(.*?)</script>`)
	m := re.FindStringSubmatch(rawHTML)
	if len(m) < 2 {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &data); err != nil {
		return ""
	}
	if media := normalizeProviderMediaURL(pageURL, mapString(data, "videoSrc", "videoUrl", "playUrl")); media != "" {
		return media
	}
	eps, _ := data["epPlaySrcs"].(map[string]any)
	if len(eps) == 0 {
		return ""
	}
	preferred := firstNonEmpty(mapString(data, "ep", "episode"))
	if m := regexp.MustCompile(`/ep-(\d+)(?:/|$)`).FindStringSubmatch(pageURL); len(m) > 1 {
		preferred = m[1]
	}
	if preferred != "" {
		if media := normalizeProviderMediaURL(pageURL, fmt.Sprint(eps[preferred])); media != "" {
			return media
		}
	}
	keys := make([]string, 0, len(eps))
	for key := range eps {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool { return episodeIndex(keys[i], i+1) < episodeIndex(keys[j], j+1) })
	for _, key := range keys {
		if media := normalizeProviderMediaURL(pageURL, fmt.Sprint(eps[key])); media != "" {
			return media
		}
	}
	return ""
}

func parseDataHLS(rawHTML, pageURL string) string {
	if m := reDataHLS.FindStringSubmatch(rawHTML); len(m) > 1 {
		return normalizeProviderMediaURL(pageURL, m[1])
	}
	return ""
}

func sortProviderEpisodes(episodes []providerEpisode) {
	sort.SliceStable(episodes, func(i, j int) bool {
		if episodes[i].Index != episodes[j].Index {
			return episodes[i].Index < episodes[j].Index
		}
		return episodes[i].Key < episodes[j].Key
	})
}

func sortProviderChapters(chapters []Chapter) {
	sort.SliceStable(chapters, func(i, j int) bool {
		ei := chapterEpisodeNumber(chapters[i], i+1)
		ej := chapterEpisodeNumber(chapters[j], j+1)
		if ei != ej {
			return ei < ej
		}
		return chapters[i].ID < chapters[j].ID
	})
}

func chapterEpisodeNumber(ch Chapter, fallback int) int {
	if len(ch.CurrentEpisode) > 0 && string(ch.CurrentEpisode) != "null" {
		var n int
		if err := json.Unmarshal(ch.CurrentEpisode, &n); err == nil && n > 0 {
			return n
		}
		var s string
		if err := json.Unmarshal(ch.CurrentEpisode, &s); err == nil {
			return episodeIndex(s, fallback)
		}
	}
	return episodeIndex(ch.Title, fallback)
}

func rawEpisode(n int) json.RawMessage {
	if n <= 0 {
		n = 1
	}
	return json.RawMessage(strconv.Itoa(n))
}

func episodeIndex(s string, fallback int) int {
	if m := reEpisodeNumber.FindStringSubmatch(s); len(m) > 1 {
		for _, group := range m[1:] {
			if group == "" {
				continue
			}
			if n, err := strconv.Atoi(group); err == nil && n > 0 {
				return n
			}
		}
	}
	return fallback
}

func nextUnusedEpisodeIndex(preferred int, used map[int]bool) int {
	if preferred <= 0 {
		preferred = 1
	}
	if !used[preferred] {
		return preferred
	}
	for i := 1; ; i++ {
		if !used[i] {
			return i
		}
	}
}

func aiEpisodeKey(href, sourceID string) string {
	u, err := url.Parse(html.UnescapeString(href))
	path := href
	if err == nil {
		path = u.Path
	}
	path = strings.Trim(strings.TrimPrefix(path, "/video/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) > 1 && parts[0] == sourceID {
		return cleanID(parts[len(parts)-1])
	}
	if len(parts) == 1 && parts[0] == sourceID {
		return ""
	}
	return cleanID(strings.Join(parts, "-"))
}

func huangguoVideoSourceID(href string) (path, sourceID string) {
	u, err := url.Parse(html.UnescapeString(href))
	if err == nil {
		path = strings.Trim(u.Path, "/")
	} else {
		path = strings.Trim(href, "/")
	}
	parts := strings.Split(path, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "series" || parts[i] == "video" {
			code := cleanID(parts[i+1])
			if code == "" {
				return "", ""
			}
			return parts[i] + "/" + code, parts[i] + "/" + code
		}
	}
	return "", ""
}

func detailIDFromString(s string) string {
	u, err := url.Parse(html.UnescapeString(s))
	path := s
	if err == nil {
		path = u.Path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "detail" {
			return cleanID(parts[i+1])
		}
	}
	return ""
}

func cleanID(s string) string {
	s = strings.TrimSpace(html.UnescapeString(s))
	s = strings.Trim(s, " /\t\r\n\"'")
	if q := strings.IndexAny(s, "?#"); q >= 0 {
		s = s[:q]
	}
	return s
}

func extractPageTitle(rawHTML string) string {
	if m := reH1Tag.FindStringSubmatch(rawHTML); len(m) > 1 {
		if t := cleanText(m[1]); t != "" {
			return t
		}
	}
	if m := regexp.MustCompile(`(?is)<meta\b[^>]*property=["']og:title["'][^>]*content=["']([^"']+)["'][^>]*>`).FindStringSubmatch(rawHTML); len(m) > 1 {
		if t := strings.TrimSpace(html.UnescapeString(m[1])); t != "" {
			return t
		}
	}
	if m := reTitleTag.FindStringSubmatch(rawHTML); len(m) > 1 {
		return cleanText(m[1])
	}
	return ""
}

func extractDescription(rawHTML string) string {
	if m := reDescMeta.FindStringSubmatch(rawHTML); len(m) > 1 {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}

func extractAttr(block string, names ...string) string {
	for _, name := range names {
		re := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*["']([^"']+)["']`)
		if m := re.FindStringSubmatch(block); len(m) > 1 {
			return strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	return ""
}

func extractByClassText(raw, className string) string {
	if className == "" {
		return ""
	}
	re := regexp.MustCompile(`(?is)<[^>]+class=["'][^"']*` + regexp.QuoteMeta(className) + `[^"']*["'][^>]*>(.*?)</[^>]+>`)
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return cleanText(m[1])
	}
	return ""
}

func firstMatchText(re *regexp.Regexp, raw string) string {
	if m := re.FindString(raw); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

func extractTags(raw string) []string {
	re := regexp.MustCompile(`(?is)<[^>]+class=["'][^"']*hg-tag[^"']*["'][^>]*>(.*?)</[^>]+>`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		if len(m) < 2 {
			continue
		}
		v := cleanText(m[1])
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	matches := reDateText.FindAllString(s, -1)
	for _, m := range matches {
		parts := strings.Split(m, "-")
		if len(parts) != 3 {
			continue
		}
		y, yErr := strconv.Atoi(parts[0])
		mo, moErr := strconv.Atoi(parts[1])
		d, dErr := strconv.Atoi(parts[2])
		if yErr == nil && moErr == nil && dErr == nil && y >= 2000 && y <= 2100 && mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			return fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
		}
	}
	return ""
}

func normalizeViews(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "次播放") {
		return s
	}
	if strings.ContainsAny(strings.ToLower(s), "w万") {
		return s + "次播放"
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return s
	}
	return strconv.FormatFloat(n, 'f', -1, 64) + "次播放"
}

func cleanText(s string) string {
	s = regexp.MustCompile(`(?is)<script\b.*?</script>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`(?is)<style\b.*?</style>`).ReplaceAllString(s, " ")
	s = regexp.MustCompile(`(?is)<[^>]+>`).ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = spaceChars.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func contextBlock(s string, start, end, padding int) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	lo := start - padding
	if lo < 0 {
		lo = 0
	}
	hi := end + padding
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func mapString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch x := v.(type) {
			case string:
				if text := strings.TrimSpace(x); text != "" {
					return text
				}
			case bool:
				return strconv.FormatBool(x)
			case float64:
				if x == float64(int64(x)) {
					return strconv.FormatInt(int64(x), 10)
				}
				return strconv.FormatFloat(x, 'f', -1, 64)
			case int:
				return strconv.Itoa(x)
			case json.Number:
				return x.String()
			}
		}
	}
	return ""
}

func mapStringSlice(m map[string]any, keys ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case string:
			for _, part := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == '/' || r == '，' || r == '、' }) {
				add(part)
			}
		case []any:
			for _, item := range x {
				if row, ok := item.(map[string]any); ok {
					if name := mapString(row, "name", "title", "label", "tag"); name != "" {
						add(name)
					}
					continue
				}
				add(fmt.Sprint(item))
			}
		}
	}
	return out
}

func extractClosestAttr(raw string, start, end int, names ...string) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(raw) {
		end = len(raw)
	}
	for _, value := range []string{raw[start:end], contextBlock(raw, start, end, 600)} {
		if value == "" {
			continue
		}
		if attr := extractAttr(value, names...); attr != "" {
			return attr
		}
	}
	return ""
}

func titleNearDetail(raw, sourceID string) string {
	needle := "/detail/" + sourceID
	idx := strings.Index(raw, needle)
	if idx < 0 {
		return ""
	}
	return extractByClassText(contextBlock(raw, idx, idx+len(needle), 1800), "hg-drama-card__title")
}

func resolveProviderURL(baseURL, ref string) string {
	ref = strings.TrimSpace(html.UnescapeString(ref))
	if ref == "" {
		return ""
	}
	ref = strings.ReplaceAll(ref, `\/`, `/`)
	if strings.HasPrefix(ref, "//") {
		return "https:" + ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if u.IsAbs() {
		if u.Scheme == "http" {
			u.Scheme = "https"
		}
		return u.String()
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}
	resolved := base.ResolveReference(u)
	if resolved.Scheme == "http" {
		resolved.Scheme = "https"
	}
	return resolved.String()
}

func normalizeProviderMediaURL(pageURL, raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	raw = strings.Trim(raw, " \t\r\n\"'")
	if raw == "" {
		return ""
	}
	if unquoted, err := strconv.Unquote("\"" + strings.ReplaceAll(raw, "\"", "\\\"") + "\""); err == nil {
		raw = unquoted
	}
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	resolved := resolveProviderURL(pageURL, raw)
	if !strings.Contains(strings.ToLower(resolved), ".m3u8") && !strings.Contains(strings.ToLower(resolved), ".mp4") {
		return ""
	}
	return resolved
}

func isProviderHTTPMediaURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}
