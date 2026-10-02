package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	dsdPlayerAssignment = regexp.MustCompile(`\bplayer_[a-zA-Z0-9_]+\s*=\s*`)
	dsdVideoDirectory   = regexp.MustCompile(`^/video[0-9]*/`)
	dsdPosterFilename   = regexp.MustCompile(`(?i)/1\.(?:jpg|jpeg|png|webp)$`)
	dsdScriptBlock      = regexp.MustCompile(`(?is)<script\b[^>]*>(.*?)</script>`)
	dsdVPathAssignment  = regexp.MustCompile(`\bvar\s+vPath\s*=\s*["']([^"']+)["']`)
)

func parseDSDPlayer(document *html.Node) (map[string]any, bool, error) {
	for _, script := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "script" }) {
		if script.FirstChild == nil {
			continue
		}
		body := script.FirstChild.Data
		for _, location := range dsdPlayerAssignment.FindAllStringIndex(body, 8) {
			decoder := json.NewDecoder(strings.NewReader(body[location[1]:]))
			decoder.UseNumber()
			var player map[string]any
			if decoder.Decode(&player) != nil || player == nil {
				return nil, true, errors.New("帝果播放器参数格式无效")
			}
			if player["id"] != nil && player["url"] != nil {
				return player, true, nil
			}
		}
	}
	return nil, false, nil
}

func dsdPlayerIdentity(player map[string]any, sourceID string) (int, int, error) {
	line, lineValid := webProviderInteger(player["sid"], 1000000)
	episode, episodeValid := webProviderInteger(player["nid"], 1000000)
	if mapString(player, "id") != sourceID || !lineValid || line < 1 || !episodeValid || episode < 1 {
		return 0, 0, errors.New("帝果播放器与请求视频不符")
	}
	return line, episode, nil
}

func dsdEpisodePage(site, sourceID string, line, episode int) string {
	return fmt.Sprintf("%s/index.php/vod/play/id/%s/sid/%d/nid/%d.html", site, sourceID, line, episode)
}

func dsdChapter(sourceID string, line, episode int, title, site string, vip bool) Chapter {
	key := fmt.Sprintf("%d-%d", line, episode)
	return Chapter{ID: providerChapterID(sourceDSD, sourceID, key), Source: sourceDSD,
		Title: truncate(firstNonEmpty(title, fmt.Sprintf("第 %d 集", episode)), 256), CurrentEpisode: rawEpisode(episode),
		PageURL: dsdEpisodePage(site, sourceID, line, episode), Referer: site + "/", VIP: vip}
}

func dsdDetailIdentity(document *html.Node, sourceID string, player map[string]any, found bool) error {
	if found {
		_, _, err := dsdPlayerIdentity(player, sourceID)
		return err
	}
	info := providerHTMLFirstClass(document, "video-info")
	for _, node := range providerHTMLNodes(info, func(node *html.Node) bool {
		return providerHTMLAttr(node, "data-id") == sourceID
	}) {
		if providerHTMLClass(node, "btn-favs") {
			return nil
		}
	}
	return errors.New("帝果未返回所请求视频的详情")
}

func parseDSDDetail(document *html.Node, pageURL, sourceID, site string) (Drama, []Chapter, error) {
	player, found, err := parseDSDPlayer(document)
	if err != nil {
		return Drama{}, nil, err
	}
	if err := dsdDetailIdentity(document, sourceID, player, found); err != nil {
		return Drama{}, nil, err
	}
	line, current := 1, 1
	if found {
		line, current, _ = dsdPlayerIdentity(player, sourceID)
	}
	info := providerHTMLFirstClass(document, "video-info")
	box := providerHTMLFirstClass(document, "video-box")
	data, _ := player["vod_data"].(map[string]any)
	title := mapString(data, "vod_name")
	if title == "" {
		for _, heading := range providerHTMLNodes(info, func(node *html.Node) bool {
			return node.Data == "h1" || node.Data == "h2" || node.Data == "h3"
		}) {
			if title = providerHTMLText(heading); title != "" {
				break
			}
		}
	}
	if title == "" {
		return Drama{}, nil, errors.New("帝果详情缺少视频名称")
	}
	cover := firstNonEmpty(providerCoverAddress(player["poster"], pageURL), dsdCardCover(box, pageURL))
	description := truncate(providerHTMLText(providerHTMLFirstClass(info, "desc")), 12000)
	drama := Drama{ID: providerDramaID(sourceDSD, sourceID), Source: sourceDSD, SourceID: sourceID,
		Title: truncate(title, 1024), Name: truncate(title, 1024), Desc: description, Intro: description,
		Cover: cover, CoverURL: cover, ChannelName: "帝果"}
	vip := providerHTMLFirstClass(box, "noVip", "openVip") != nil
	if vip {
		drama.VIP = &vip
	}
	seenTags := map[string]bool{}
	addTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag != "" && len([]rune(tag)) <= 48 && len(drama.Tags) < 32 && !seenTags[tag] {
			seenTags[tag] = true
			drama.Tags = append(drama.Tags, tag)
		}
	}
	for _, tag := range strings.Split(mapString(data, "vod_class"), ",") {
		addTag(tag)
	}
	for _, tag := range providerHTMLNodes(providerHTMLFirstClass(info, "labels"), func(node *html.Node) bool { return node.Data == "a" }) {
		addTag(providerHTMLText(tag))
	}
	chapters := []Chapter{dsdChapter(sourceID, line, current, "", site, vip)}
	seen := map[int]bool{current: true}
	addEpisode := func(reference, title string) {
		action, values, valid := dsdRouteParameters(pageURL, reference)
		candidateLine, lineValid := webProviderInteger(values["sid"], 1000000)
		episode, episodeValid := webProviderInteger(values["nid"], 1000000)
		if !valid || action != "play" || values["id"] != sourceID || !lineValid || candidateLine != line || !episodeValid || episode < 1 || seen[episode] {
			return
		}
		seen[episode] = true
		chapters = append(chapters, dsdChapter(sourceID, line, episode, title, site, vip))
	}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "a" }) {
		addEpisode(providerHTMLAttr(anchor, "href"), providerHTMLText(anchor))
	}
	addEpisode(mapString(player, "link_pre"), "")
	addEpisode(mapString(player, "link_next"), "")
	if len(chapters) > 10000 {
		return Drama{}, nil, errors.New("帝果分集目录过大")
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(i + 1))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(j + 1))
		return left < right
	})
	if len(chapters) == 1 {
		chapters[0].Title = "正片"
	}
	drama.TotalEpisode, drama.EpisodeCount = len(chapters), len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) fetchDSDDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !webProviderNumericID.MatchString(sourceID) {
		return Drama{}, nil, errors.New("帝果视频 ID 无效")
	}
	site := d.providerBaseURL(sourceDSD)
	document, address, err := d.fetchProviderPage(ctx, dsdEpisodePage(site, sourceID, 1, 1), site+"/", dsdUserAgent)
	if err != nil {
		return Drama{}, nil, err
	}
	return parseDSDDetail(document, address, sourceID, site)
}

func dsdIssuedMedia(player map[string]any, pageURL string) (string, error) {
	address := mapString(player, "url")
	if address == "" {
		return "", nil
	}
	switch mapString(player, "encrypt") {
	case "", "0":
	case "1":
		decoded, err := url.PathUnescape(address)
		if err != nil {
			return "", errors.New("帝果播放地址编码无效")
		}
		address = decoded
	case "2":
		decoded, err := base64.StdEncoding.DecodeString(address)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(address)
		}
		if err != nil {
			return "", errors.New("帝果播放地址编码无效")
		}
		address, err = url.PathUnescape(string(decoded))
		if err != nil {
			return "", errors.New("帝果播放地址编码无效")
		}
	default:
		return "", errors.New("帝果播放地址编码已变化")
	}
	address = resolveProviderURL(pageURL, address)
	parsed, err := url.Parse(address)
	if err != nil || !isProviderHTTPMediaURL(address) || parsed.User != nil {
		return "", errors.New("帝果播放地址无效")
	}
	path := strings.ToLower(parsed.Path)
	if !strings.HasSuffix(path, ".m3u8") && !strings.HasSuffix(path, ".mp4") {
		return "", errors.New("帝果暂未提供可直接播放的媒体")
	}
	return address, nil
}

func dsdSplitTopLevel(value string) []string {
	var parts []string
	depth, start := 0, 0
	for index, char := range value {
		switch char {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '+':
			if depth == 0 {
				if part := strings.TrimSpace(value[start:index]); part != "" {
					parts = append(parts, part)
				}
				start = index + 1
			}
		}
	}
	if part := strings.TrimSpace(value[start:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

type dsdExpressionParser struct {
	text  string
	index int
	depth int
}

func (parser *dsdExpressionParser) skipSpaces() {
	for parser.index < len(parser.text) && strings.ContainsRune(" \t\r\n", rune(parser.text[parser.index])) {
		parser.index++
	}
}

func (parser *dsdExpressionParser) parseExpression() (int, bool) {
	value, ok := parser.parseTerm()
	if !ok {
		return 0, false
	}
	for {
		parser.skipSpaces()
		if parser.index >= len(parser.text) || parser.text[parser.index] != '+' && parser.text[parser.index] != '-' {
			return value, true
		}
		operator := parser.text[parser.index]
		parser.index++
		right, ok := parser.parseTerm()
		if !ok {
			return 0, false
		}
		if operator == '+' {
			value += right
		} else {
			value -= right
		}
	}
}

func (parser *dsdExpressionParser) parseTerm() (int, bool) {
	value, ok := parser.parseFactor()
	if !ok {
		return 0, false
	}
	for {
		parser.skipSpaces()
		if parser.index >= len(parser.text) || !strings.ContainsRune("*/%", rune(parser.text[parser.index])) {
			return value, true
		}
		operator := parser.text[parser.index]
		parser.index++
		right, ok := parser.parseFactor()
		if !ok || right == 0 && operator != '*' {
			return 0, false
		}
		switch operator {
		case '*':
			value *= right
		case '/':
			if value%right != 0 {
				return 0, false
			}
			value /= right
		case '%':
			value %= right
		}
	}
}

func (parser *dsdExpressionParser) parseFactor() (int, bool) {
	parser.skipSpaces()
	sign := 1
	for parser.index < len(parser.text) && (parser.text[parser.index] == '+' || parser.text[parser.index] == '-') {
		if parser.text[parser.index] == '-' {
			sign = -sign
		}
		parser.index++
		parser.skipSpaces()
	}
	if parser.index >= len(parser.text) {
		return 0, false
	}
	if parser.text[parser.index] == '(' {
		if parser.depth >= 32 {
			return 0, false
		}
		parser.depth++
		parser.index++
		value, ok := parser.parseExpression()
		parser.depth--
		parser.skipSpaces()
		if !ok || parser.index >= len(parser.text) || parser.text[parser.index] != ')' {
			return 0, false
		}
		parser.index++
		return sign * value, true
	}
	start := parser.index
	for parser.index < len(parser.text) && parser.text[parser.index] >= '0' && parser.text[parser.index] <= '9' {
		parser.index++
	}
	if start == parser.index {
		return 0, false
	}
	value, err := strconv.Atoi(parser.text[start:parser.index])
	return sign * value, err == nil
}

func dsdEvaluateDigitExpression(value string) (int, bool) {
	if len(value) > 4096 || strings.Trim(value, "0123456789()+-*/% \t\r\n") != "" {
		return 0, false
	}
	parser := &dsdExpressionParser{text: value}
	result, ok := parser.parseExpression()
	parser.skipSpaces()
	return result, ok && parser.index == len(parser.text)
}

func dsdAaDigit(part string) (string, bool) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", false
	}
	if strings.Contains(part, "(oﾟｰﾟo)") {
		return "u", true
	}
	if strings.Contains(part, "(ﾟДﾟ).ﾟΘﾟﾉ") {
		return "b", true
	}
	replacements := []struct {
		old string
		new string
	}{
		{"(c^_^o)", "0"},
		{"(ﾟΘﾟ)", "1"},
		{"(o^_^o)", "3"},
		{"(ﾟｰﾟ)", "4"},
	}
	for _, replacement := range replacements {
		part = strings.ReplaceAll(part, replacement.old, replacement.new)
	}
	value, ok := dsdEvaluateDigitExpression(part)
	if !ok {
		return "", false
	}
	return strconv.Itoa(value), true
}

func decodeDSDAaencode(raw string) (string, bool) {
	if len(raw) > 1<<20 {
		return "", false
	}
	const marker = "(ﾟДﾟ) ['_'] ( (ﾟДﾟ) ['_']"
	const tailMarker = "+ (ﾟДﾟ)[ﾟoﾟ]"
	start := strings.Index(raw, marker)
	if start < 0 {
		return "", false
	}
	body := raw[start:]
	end := strings.LastIndex(body, tailMarker)
	if end < 0 {
		return "", false
	}
	tokens := strings.Split(body[:end], "(ﾟДﾟ)[ﾟεﾟ]+")
	var decoded strings.Builder
	for _, token := range tokens[1:] {
		if decoded.Len() > 64<<10 {
			return "", false
		}
		var digits strings.Builder
		for _, part := range dsdSplitTopLevel(token) {
			digit, ok := dsdAaDigit(part)
			if !ok {
				return "", false
			}
			digits.WriteString(digit)
		}
		value := digits.String()
		if strings.HasPrefix(value, "u") && len(value) == 5 {
			code, err := strconv.ParseInt(value[1:], 16, 32)
			if err != nil {
				return "", false
			}
			decoded.WriteRune(rune(code))
		} else if value != "" && strings.Trim(value, "01234567") == "" {
			code, err := strconv.ParseInt(value, 8, 32)
			if err != nil {
				return "", false
			}
			decoded.WriteRune(rune(code))
		}
	}
	return decoded.String(), true
}

func dsdSignedPathFromVplayer(body string) (string, bool) {
	for _, match := range dsdScriptBlock.FindAllStringSubmatch(body, -1) {
		if !strings.Contains(match[1], "(ﾟДﾟ)") {
			continue
		}
		decoded, ok := decodeDSDAaencode(match[1])
		if !ok {
			continue
		}
		if found := dsdVPathAssignment.FindStringSubmatch(decoded); len(found) > 1 {
			return strings.TrimSpace(found[1]), true
		}
	}
	return "", false
}

func dsdVplayerMediaPath(address string) string {
	if parsed, err := url.Parse(address); err == nil && parsed.IsAbs() {
		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		if parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
		return path
	}
	return address
}

func dsdVplayerReferer(site string) string {
	return strings.TrimRight(site, "/") + "/addons/vplayer/"
}

func (d *Downloader) signDSDMedia(ctx context.Context, address, pageURL, site string) (string, bool, error) {
	if strings.Contains(address, "sign=") {
		return address, false, nil
	}
	mediaPath := dsdVplayerMediaPath(address)
	vplayerURL := strings.TrimRight(site, "/") + "/addons/vplayer/?url=" + url.QueryEscape(mediaPath) + "&jump="
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, dsdUserAgent)
	body, err := d.fetchProviderText(pageContext, vplayerURL, site+"/")
	if err != nil {
		return "", false, err
	}
	signed, found := dsdSignedPathFromVplayer(body)
	if !found || signed == "" {
		return address, false, nil
	}
	resolved := resolveProviderURL(dsdVplayerReferer(site), signed)
	parsed, err := url.Parse(resolved)
	if err != nil || !isProviderHTTPMediaURL(resolved) || parsed.User != nil {
		return "", false, errors.New("帝果播放签名地址无效")
	}
	path := strings.ToLower(parsed.Path)
	if !strings.HasSuffix(path, ".m3u8") && !strings.HasSuffix(path, ".mp4") {
		return "", false, errors.New("帝果签名后未返回可播放媒体")
	}
	if providerMediaOrigin(parsed) == "" || pageURL == "" {
		return "", false, errors.New("帝果播放签名缺少来源信息")
	}
	return resolved, true, nil
}

func dsdMediaFromPagePath(cover, pageURL string) string {
	address, err := url.Parse(cover)
	page, pageErr := url.Parse(pageURL)
	if err != nil || pageErr != nil || !isProviderHTTPMediaURL(cover) || address.User != nil ||
		providerMediaOrigin(address) != providerMediaOrigin(page) || !dsdVideoDirectory.MatchString(address.Path) || !dsdPosterFilename.MatchString(address.Path) {
		return ""
	}
	address.Path = dsdPosterFilename.ReplaceAllString(address.Path, "/1000k/index.m3u8")
	address.RawPath, address.RawQuery, address.Fragment = "", "", ""
	return address.String()
}

func (d *Downloader) resolveDSDMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceDSD, sourceID, "")
	if !valid || source != sourceDSD || !webProviderNumericID.MatchString(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("帝果播放信息无效，请刷新详情")
	}
	parts := strings.Split(strings.TrimPrefix(task.Chapter.ID, prefix), "-")
	if len(parts) != 2 {
		return providerMedia{}, errors.New("帝果播放分集无效")
	}
	line, lineValid := webProviderInteger(parts[0], 1000000)
	episode, episodeValid := webProviderInteger(parts[1], 1000000)
	if !lineValid || line < 1 || !episodeValid || episode < 1 {
		return providerMedia{}, errors.New("帝果播放分集无效")
	}
	site := d.providerBaseURL(sourceDSD)
	document, pageURL, err := d.fetchProviderPage(ctx, dsdEpisodePage(site, sourceID, line, episode), site+"/", dsdUserAgent)
	if err != nil {
		return providerMedia{}, err
	}
	player, found, err := parseDSDPlayer(document)
	if err != nil {
		return providerMedia{}, err
	}
	if err := dsdDetailIdentity(document, sourceID, player, found); err != nil {
		return providerMedia{}, err
	}
	address := ""
	if found {
		actualLine, actualEpisode, _ := dsdPlayerIdentity(player, sourceID)
		if line != actualLine || episode != actualEpisode {
			return providerMedia{}, errors.New("帝果返回的播放地址与请求分集不符")
		}
		if preview, valid := webProviderInteger(player["trysee"], 86400); valid && preview > 0 {
			return providerMedia{}, errors.New("帝果仅提供试看，暂未取得正片播放地址")
		}
		address, err = dsdIssuedMedia(player, pageURL)
		if err != nil {
			return providerMedia{}, err
		}
	}
	if address == "" {
		if line != 1 || episode != 1 {
			return providerMedia{}, errors.New("帝果未提供该分集的播放地址")
		}
		box := providerHTMLFirstClass(document, "video-box")
		cover := firstNonEmpty(providerCoverAddress(player["poster"], pageURL), dsdCardCover(box, pageURL))
		address = dsdMediaFromPagePath(cover, pageURL)
	}
	if address == "" {
		return providerMedia{}, errors.New("帝果未提供播放地址，请重试或确认站源权限")
	}
	signed, signedByVplayer, err := d.signDSDMedia(ctx, address, pageURL, site)
	if err != nil {
		return providerMedia{}, err
	}
	address = signed
	parsed, _ := url.Parse(address)
	referer := pageURL
	if signedByVplayer || strings.Contains(address, "sign=") {
		referer = dsdVplayerReferer(site)
	}
	media := providerMedia{URL: address, Referer: referer, credentials: &providerMediaCredentials{
		origin: providerMediaOrigin(parsed), referer: referer, userAgent: dsdUserAgent,
	}}
	return d.prepareWebProviderMedia(ctx, media, "帝果")
}
