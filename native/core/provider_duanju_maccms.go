package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	maccmsPlayerData     = regexp.MustCompile(`(?s)player_aaaa\s*=\s*(\{.*?\})\s*</script>`)
	maccmsPlayerURLField = regexp.MustCompile(`(?i)"url"\s*:\s*"([^"]+)"`)
)

type maccmsSourceProfile struct {
	Source      string
	CategoryURL func(base, category string) string
	SearchURL   func(base, query string, page int) string
	PageURL     func(base, category string, page int) string
	Episodes    func(document *html.Node) []providerEpisode
}

var maccmsEpisodeLink = regexp.MustCompile(`(?i)/(?:vod/)?play/|/vodplay/|/drama-play|episode_id=`)

var maccmsDetailLink = regexp.MustCompile(`(?i)/(?:index\.php/vod/)?(?:detail|vod|show|view|movie|drama|zywview|xzyxvd)/`)

func maccmsEpisodeNodes(document *html.Node) []*html.Node {
	var nodes []*html.Node
	collect := func(match func(*html.Node) bool) {
		for _, list := range providerHTMLNodes(document, match) {
			nodes = append(nodes, providerHTMLNodes(list, func(node *html.Node) bool { return node.Data == "a" })...)
		}
	}
	for _, name := range []string{"content__playlist", "playlink", "pcDrama_catalogItem", "catalogItem", "playlist"} {
		collect(maccmsClassMatcher(name))
	}
	if len(nodes) == 0 {
		for _, name := range []string{"tab-pane", "playlist", "playList"} {
			collect(maccmsClassMatcher(name))
		}
	}
	if len(nodes) == 0 {
		nodes = providerHTMLNodes(document, func(node *html.Node) bool {
			return node.Data == "a" && maccmsEpisodeLink.MatchString(providerHTMLAttr(node, "href"))
		})
	}
	return nodes
}

func maccmsEpisodesFromDocument(document *html.Node) []providerEpisode {
	var episodes []providerEpisode
	seen := map[string]bool{}
	for index, anchor := range maccmsEpisodeNodes(document) {
		link := strings.TrimSpace(providerHTMLAttr(anchor, "href"))
		title := providerHTMLText(anchor)
		if link == "" {
			continue
		}
		if strings.Contains(title, "APP") || strings.Contains(title, "下载") {
			continue
		}
		key := link
		if seen[key] {
			continue
		}
		seen[key] = true
		episodes = append(episodes, providerEpisode{
			Key:   strconv.Itoa(index + 1),
			Title: title,
			URL:   link,
			Index: index + 1,
		})
	}
	return episodes
}

func maccmsCardCover(card *html.Node, pageURL string) string {
	for _, image := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "img" }) {
		for _, attribute := range []string{"data-original", "data-src", "src"} {
			if address := providerCoverAddress(providerHTMLAttr(image, attribute), pageURL); address != "" {
				return address
			}
		}
	}
	for _, anchor := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "a" }) {
		for _, attribute := range []string{"data-original", "data-src"} {
			if address := providerCoverAddress(providerHTMLAttr(anchor, attribute), pageURL); address != "" {
				return address
			}
		}
	}
	return ""
}

func maccmsCardTitle(card *html.Node) string {
	for _, attribute := range []string{"title", "alt"} {
		for _, node := range providerHTMLNodes(card, func(node *html.Node) bool {
			return node.Data == "a" || node.Data == "img"
		}) {
			if text := strings.TrimSpace(providerHTMLAttr(node, attribute)); text != "" {
				return text
			}
		}
	}
	for _, node := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "a" }) {
		if text := providerHTMLText(node); text != "" {
			return text
		}
	}
	return ""
}

func maccmsCardLink(card *html.Node, base string) string {
	for _, anchor := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "a" }) {
		link := providerHTMLAttr(anchor, "href")
		if link == "" || strings.Contains(link, "javascript:") {
			continue
		}
		if address := duanjuAbsolute(base, link); address != "" {
			return address
		}
	}
	return ""
}

func maccmsCardRemark(card *html.Node) string {
	for _, name := range []string{"meta-post-type2", "imagelabel", "module-item-note", "pic-text", "lastChapter", "SecondList_totalChapterNum", "SecondList_bookType"} {
		if node := providerHTMLFirstClass(card, name); node != nil {
			if text := providerHTMLText(node); text != "" {
				return text
			}
		}
	}
	return ""
}

func maccmsCards(document *html.Node, source, base string) []Drama {
	var cards []*html.Node
	seen := map[*html.Node]bool{}
	for _, name := range maccmsCardClasses {
		for _, node := range providerHTMLNodes(document, maccmsClassMatcher(name)) {
			if !seen[node] {
				seen[node] = true
				cards = append(cards, node)
			}
		}
	}
	if len(cards) == 0 {
		cards = maccmsAnchorCards(document)
	}
	var items []Drama
	ids := map[string]bool{}
	for _, card := range cards {
		link := maccmsCardLink(card, base)
		title := maccmsCardTitle(card)
		if link == "" || title == "" {
			continue
		}
		sourceID := maccmsSourceIDFromURL(link)
		if sourceID == "" || ids[sourceID] {
			continue
		}
		ids[sourceID] = true
		remark := maccmsCardRemark(card)
		items = append(items, Drama{
			ID:           providerDramaID(source, sourceID),
			Source:       source,
			SourceID:     sourceID,
			Title:        title,
			Cover:        maccmsCardCover(card, link),
			Remark:       remark,
			EpisodeCount: json.Number(strconv.Itoa(duanjuEpisodeNumber(remark, 0))),
			ChannelName:  duanjuSourceName(source),
		})
	}
	return items
}

func maccmsClassMatcher(name string) func(*html.Node) bool {
	return func(node *html.Node) bool {
		for _, value := range strings.Fields(providerHTMLAttr(node, "class")) {
			if value == name || strings.HasSuffix(value, "-"+name) {
				return true
			}
		}
		return false
	}
}

func maccmsAnchorCards(document *html.Node) []*html.Node {
	var cards []*html.Node
	seen := map[*html.Node]bool{}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Data == "a" && maccmsDetailLink.MatchString(providerHTMLAttr(node, "href"))
	}) {
		for parent := anchor.Parent; parent != nil && parent.Type == html.ElementNode; parent = parent.Parent {
			if seen[parent] {
				break
			}
			if parent.Data != "li" && parent.Data != "div" {
				continue
			}
			seen[parent] = true
			cards = append(cards, parent)
		}
	}
	return cards
}

var maccmsCardClasses = []string{
	"col-lg-2", "col-xl-2", "module-item", "module-poster-item", "public-list-box",
	"videoBox", "detail-list-item", "col-md-6", "col-6", "listItem", "FeaturedList_featuredItem",
	"BrowseList_listItem", "SecondList_secondListItem", "vodlist__item", "v_list",
	"entry-wrapper", "TagBookList_tagItem",
}

var maccmsDetailPath = regexp.MustCompile(`(?i)/(?:voddetail|detail|show|vod|drama|movie|tv)/([0-9]+)(?:[-./]|$)`)

func maccmsSourceIDFromURL(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	path := strings.TrimSuffix(parsed.Path, ".html")
	if matches := maccmsDetailPath.FindStringSubmatch(path + "/"); len(matches) > 1 {
		return matches[1]
	}
	cleaned := strings.Trim(path, "/")
	parts := strings.Split(cleaned, "/")
	for index := len(parts) - 1; index >= 0; index-- {
		candidate := strings.TrimSuffix(parts[index], ".html")
		if webProviderNumericID.MatchString(candidate) {
			return candidate
		}
		if _, tail, found := strings.Cut(candidate, "-"); found && webProviderNumericID.MatchString(tail) {
			return tail
		}
	}
	if id := parsed.Query().Get("id"); webProviderNumericID.MatchString(id) {
		return id
	}
	return ""
}

func (d *Downloader) fetchMaccmsCatalogPage(ctx context.Context, source string, page int, category string) ([]Drama, bool, error) {
	base := d.duanjuBaseURL(source)
	address := ""
	switch source {
	case sourceHuaguo:
		if page <= 1 && strings.TrimSpace(category) == "" {
			address = base + "/"
		} else {
			class := strings.TrimSpace(category)
			if class == "" {
				class = "27"
			}
			address = fmt.Sprintf("%s/search.html?page=%d&searchtype=5&tid=%s&year=", base, page, url.QueryEscape(class))
		}
	case sourceFaguo:
		class := strings.TrimSpace(category)
		if class == "" {
			address = fmt.Sprintf("%s/xzyxvt/%dzmn.html", base, page)
		} else if strings.Contains(class, "zmn") {
			address = base + strings.SplitN(class, "zmn", 2)[0] + strconv.Itoa(page) + "zmn.html"
		} else {
			return nil, false, errors.New("发果分类无效")
		}
	case sourceWuguo:
		class := strings.TrimSpace(category)
		if class == "" {
			address = base + "/"
		} else {
			address = fmt.Sprintf("%s%s/page/%d.html", base, strings.TrimSuffix(class, ".html"), page)
		}
	case sourceWangguo:
		class := strings.TrimSpace(category)
		if class == "" {
			address = fmt.Sprintf("%s/show/duanju-----------.html", base)
		} else {
			address = fmt.Sprintf("%s%s%d---.html", base, strings.SplitN(class, "---.html", 2)[0], page)
		}
	case sourcePiguo:
		class := strings.TrimSpace(category)
		if class == "" {
			class = "67"
		}
		if page <= 1 {
			address = fmt.Sprintf("%s/p/66/c/%s", base, url.PathEscape(class))
		} else {
			address = fmt.Sprintf("%s/p/66/c/%s?year=&page=%d", base, url.PathEscape(class), page)
		}
	default:
		return nil, false, errors.New("该站源没有网页目录")
	}
	document, finalURL, err := d.fetchProviderPage(ctx, address, base+"/", duanjuUserAgent)
	if err != nil {
		return nil, false, err
	}
	items := maccmsCards(document, source, base)
	_ = finalURL
	return items, len(items) > 0, nil
}

func (d *Downloader) fetchMaccmsDetail(ctx context.Context, source, sourceID string) (Drama, []Chapter, error) {
	base := d.duanjuBaseURL(source)
	candidates := maccmsDetailCandidates(source, base, sourceID)
	var lastErr error
	for _, address := range candidates {
		document, finalURL, err := d.fetchProviderPage(ctx, address, base+"/", duanjuUserAgent)
		if err != nil {
			lastErr = err
			continue
		}
		episodes := maccmsEpisodesFromDocument(document)
		if len(episodes) == 0 {
			lastErr = errors.New("未解析到分集列表")
			continue
		}
		drama := Drama{
			ID:          providerDramaID(source, sourceID),
			Source:      source,
			SourceID:    sourceID,
			Title:       maccmsDetailTitle(document),
			Intro:       maccmsDetailIntro(document),
			Cover:       maccmsDetailCover(document, finalURL),
			Category:    maccmsDetailCategory(document),
			ChannelName: duanjuSourceName(source),
		}
		var chapters []Chapter
		for index, episode := range episodes {
			number := duanjuEpisodeNumber(firstNonEmpty(episode.Title, episode.Key), index+1)
			link := duanjuAbsolute(base, episode.URL)
			if link == "" {
				continue
			}
			chapters = append(chapters, duanjuChapter(source, sourceID, number, episode.Title, link, link, base+"/"))
		}
		if len(chapters) == 0 {
			lastErr = errors.New("未解析到可播放分集")
			continue
		}
		sortDuanjuChapters(chapters)
		drama.EpisodeCount = json.Number(strconv.Itoa(len(chapters)))
		return drama, chapters, nil
	}
	if lastErr == nil {
		lastErr = errors.New("未找到该剧的详情页")
	}
	return Drama{}, nil, lastErr
}

func maccmsDetailCandidates(source, base, sourceID string) []string {
	switch source {
	case sourceFaguo:
		return []string{
			fmt.Sprintf("%s/xzyxvd/%s.html", base, sourceID),
			fmt.Sprintf("%s/detail/%s.html", base, sourceID),
			fmt.Sprintf("%s/voddetail/%s.html", base, sourceID),
		}
	case sourceWuguo:
		return []string{
			fmt.Sprintf("%s/index.php/vod/detail/id/%s.html", base, sourceID),
			fmt.Sprintf("%s/dramaDetail/%s.html", base, sourceID),
			fmt.Sprintf("%s/detail/%s.html", base, sourceID),
		}
	case sourceWangguo:
		return []string{
			fmt.Sprintf("%s/vod/%s.html", base, sourceID),
			fmt.Sprintf("%s/index.php/vod/detail/id/%s.html", base, sourceID),
			fmt.Sprintf("%s/detail/%s.html", base, sourceID),
		}
	case sourcePiguo:
		return []string{
			fmt.Sprintf("%s/movie/%s", base, sourceID),
			fmt.Sprintf("%s/p/66/d/%s", base, sourceID),
		}
	case sourceHuaguo:
		return []string{
			fmt.Sprintf("%s/zywview/%s.html", base, sourceID),
			fmt.Sprintf("%s/zywdetail/%s.html", base, sourceID),
			fmt.Sprintf("%s/detail/%s.html", base, sourceID),
		}
	default:
		return nil
	}
}

func maccmsDetailTitle(document *html.Node) string {
	for _, name := range []string{"module-info-heading", "detail-title", "video-info-title", "page-title"} {
		if node := providerHTMLFirstClass(document, name); node != nil {
			if text := providerHTMLText(node); text != "" {
				return maccmsCleanTitle(text)
			}
		}
	}
	if node := providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "title" }); len(node) > 0 {
		return maccmsCleanTitle(providerHTMLText(node[0]))
	}
	return ""
}

var maccmsTitleSuffixes = []string{"在线观看", "免费观看", "高清完整版", "完整版", "全集", "在线播放", "高清", "免费"}

func maccmsCleanTitle(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if cut, _, found := strings.Cut(text, " - "); found {
		text = strings.TrimSpace(cut)
	}
	if cut, _, found := strings.Cut(text, " _ "); found {
		text = strings.TrimSpace(cut)
	}
	for _, separator := range []string{"-", "_", "|"} {
		for {
			cut, _, found := strings.Cut(text, separator)
			if !found {
				break
			}
			head := strings.TrimSpace(cut)
			if !maccmsTitleTailIsSeo(text[len(cut)+len(separator):]) {
				break
			}
			text = head
		}
	}
	for changed := true; changed; {
		changed = false
		for _, suffix := range maccmsTitleSuffixes {
			if strings.HasSuffix(text, suffix) && len(text) > len(suffix) {
				text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
				changed = true
			}
		}
	}
	text = strings.Trim(text, "-_|·— ")
	if text == "" {
		return strings.TrimSpace(raw)
	}
	return text
}

func maccmsTitleTailIsSeo(tail string) bool {
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return true
	}
	for _, marker := range []string{"短剧", "全集", "在线观看", "免费", "高清", "完整版", "视频", "剧场", "影院", "网"} {
		if strings.Contains(tail, marker) {
			return true
		}
	}
	return false
}

func maccmsDetailIntro(document *html.Node) string {
	for _, name := range []string{"module-info-introduction-content", "detail-content", "video-info-content", "introduction_introEllipsis"} {
		if node := providerHTMLFirstClass(document, name); node != nil {
			if text := providerHTMLText(node); text != "" {
				return truncate(text, 2000)
			}
		}
	}
	for _, meta := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "meta" }) {
		if strings.EqualFold(providerHTMLAttr(meta, "name"), "description") {
			if content := strings.TrimSpace(providerHTMLAttr(meta, "content")); content != "" {
				return truncate(content, 2000)
			}
		}
	}
	return ""
}

func maccmsDetailCover(document *html.Node, pageURL string) string {
	for _, name := range []string{"module-item-pic", "detail-pic", "video-info-pic", "pic"} {
		for _, node := range providerHTMLNodes(document, func(node *html.Node) bool { return providerHTMLClass(node, name) }) {
			if address := maccmsCardCover(node, pageURL); address != "" {
				return address
			}
		}
	}
	for _, image := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "img" }) {
		for _, attribute := range []string{"data-original", "data-src", "src"} {
			if address := providerCoverAddress(providerHTMLAttr(image, attribute), pageURL); address != "" {
				return address
			}
		}
	}
	return ""
}

func maccmsDetailCategory(document *html.Node) string {
	for _, name := range []string{"module-info-tag-link", "detail-tag", "video-info-actor"} {
		nodes := providerHTMLNodes(document, func(node *html.Node) bool { return providerHTMLClass(node, name) })
		if len(nodes) > 0 {
			var parts []string
			for _, anchor := range providerHTMLNodes(nodes[0], func(node *html.Node) bool { return node.Data == "a" }) {
				if text := providerHTMLText(anchor); text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				return duanjuCSV(parts)
			}
		}
	}
	return ""
}

func (d *Downloader) searchMaccms(ctx context.Context, source, query string) ([]Drama, error) {
	base := d.duanjuBaseURL(source)
	var address string
	switch source {
	case sourceHuaguo:
		address = fmt.Sprintf("%s/search.html?searchword=%s", base, url.QueryEscape(query))
	case sourceFaguo:
		address = fmt.Sprintf("%s/xzyxvc/%s-wdyswzqun1num.html", base, url.PathEscape(query))
	case sourceWuguo:
		address = fmt.Sprintf("%s/index.php/vod/search/page/1/wd/%s.html", base, url.PathEscape(query))
	case sourceWangguo:
		address = fmt.Sprintf("%s/search/%s----------1---.html", base, url.PathEscape(query))
	case sourcePiguo:
		address = fmt.Sprintf("%s/q/%s?page=1", base, url.PathEscape(query))
	default:
		return nil, errors.New("该站源不支持在线搜索")
	}
	document, _, err := d.fetchProviderPage(ctx, address, base+"/", duanjuUserAgent)
	if err != nil {
		return nil, err
	}
	return maccmsCards(document, source, base), nil
}

func maccmsPlayerURL(body string) string {
	if matches := maccmsPlayerData.FindStringSubmatch(body); len(matches) > 1 {
		if inner := maccmsPlayerURLField.FindStringSubmatch(matches[1]); len(inner) > 1 {
			return strings.ReplaceAll(inner[1], `\/`, `/`)
		}
	}
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)\$\.url\s*=\s*"([^"]+)"`),
		regexp.MustCompile(`(?i)"url"\s*:\s*"([^"]+\.(?:m3u8|mp4)[^"]*)"`),
		regexp.MustCompile(`(?i)(https?://[^\s"'<>]+\.(?:m3u8|mp4)[^\s"'<>]*)`),
	} {
		if matches := pattern.FindStringSubmatch(body); len(matches) > 1 {
			return strings.ReplaceAll(matches[1], `\/`, `/`)
		}
	}
	return ""
}

func maccmsNormalizePlaybackURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	raw = strings.Trim(raw, ",\\。，;；")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "p.") || strings.Contains(raw, "c1.") {
		parts := strings.Split(raw, "/")
		if len(parts) >= 3 {
			base := strings.Join(parts[:len(parts)-2], "/")
			folder := parts[len(parts)-2]
			if decoded := maccmsDecodeUnicode(folder); decoded != folder {
				return base + "/" + url.PathEscape(decoded) + "/index.m3u8"
			}
		}
	}
	return raw
}

var maccmsUnicodeEscape = regexp.MustCompile(`\\?u([0-9a-fA-F]{4})`)

func maccmsDecodeUnicode(value string) string {
	return maccmsUnicodeEscape.ReplaceAllStringFunc(value, func(match string) string {
		hex := match[len(match)-4:]
		if number, err := strconv.ParseInt(hex, 16, 32); err == nil {
			return string(rune(number))
		}
		return match
	})
}
