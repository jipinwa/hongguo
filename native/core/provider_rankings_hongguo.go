package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var hongguoRankingMetric = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[万亿]?热度$`)

func (d *Downloader) fetchHongguoRankingPage(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt*attempt) * 300 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return rankingPage{}, ctx.Err()
			}
		}
		base := d.providerBaseURL(sourceHongguo)
		address := base + "/rank/" + board.path
		if page > 1 {
			address += "?page=" + strconv.Itoa(page)
		}
		requestCtx := ctx
		if attempt > 0 {
			requestCtx = context.WithValue(ctx, providerTextNoCacheKey{}, true)
		}
		body, err := d.fetchProviderText(requestCtx, address, base+"/")
		if err != nil {
			return rankingPage{}, err
		}
		result, err := parseHongguoRanking(body, board, page)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, errHongguoRankingIncomplete) {
			return rankingPage{}, err
		}
		lastErr = err
	}
	return rankingPage{}, lastErr
}

func hongguoRankingURLPage(address string, board rankingBoard) (int, bool) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || strings.TrimRight(parsed.Path, "/") != "/rank/"+board.path {
		return 0, false
	}
	if parsed.Host != "" && parsed.Hostname() != "hongguoduanju.com" && parsed.Hostname() != "www.hongguoduanju.com" {
		return 0, false
	}
	value := parsed.Query().Get("page")
	if value == "" {
		return 1, true
	}
	page, err := strconv.Atoi(value)
	return page, err == nil && page >= 1 && page <= 500
}

func hongguoRankingDramaID(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || parsed.Path != "/detail" {
		return ""
	}
	if parsed.Host != "" && parsed.Hostname() != "hongguoduanju.com" && parsed.Hostname() != "www.hongguoduanju.com" {
		return ""
	}
	id := parsed.Query().Get("series_id")
	if !hongguoNumericID.MatchString(id) {
		return ""
	}
	return id
}

func parseHongguoRankingDocument(body string, board rankingBoard, page int, loader map[string]any) (rankingPage, error) {
	if len(body) > 4<<20 {
		return rankingPage{}, errHongguoRankingFormat
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	identified := len(loader) > 0
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Data == "link" && providerHTMLAttr(node, "rel") == "canonical"
	}) {
		actual, valid := hongguoRankingURLPage(providerHTMLAttr(node, "href"), board)
		if !valid || actual != page {
			return rankingPage{}, errHongguoRankingFormat
		}
		identified = true
	}
	if !identified {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	pagination := providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Data == "nav" && providerHTMLAttr(node, "aria-label") == "榜单分页"
	})
	if len(pagination) != 1 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	current := providerHTMLNodes(pagination[0], func(node *html.Node) bool {
		return providerHTMLAttr(node, "aria-current") == "page"
	})
	if len(current) != 1 || providerHTMLText(current[0]) != strconv.Itoa(page) {
		return rankingPage{}, errHongguoRankingFormat
	}
	result := rankingPage{Items: []rankingItem{}, UpdatedText: mapString(loader, "updatedText")}
	maximumPage := page
	for _, link := range providerHTMLNodes(pagination[0], func(node *html.Node) bool { return node.Data == "a" }) {
		actual, valid := hongguoRankingURLPage(providerHTMLAttr(link, "href"), board)
		if !valid {
			return rankingPage{}, errHongguoRankingFormat
		}
		if actual > maximumPage {
			maximumPage = actual
		}
		if providerHTMLAttr(link, "rel") == "next" {
			if actual != page+1 {
				return rankingPage{}, errHongguoRankingFormat
			}
			result.HasMore = true
		}
	}
	if !result.HasMore {
		if maximumPage != page {
			return rankingPage{}, errHongguoRankingFormat
		}
		result.TotalPages = page
	}
	if items, found := parseHongguoRankingJSONLD(body, board, page); found {
		result.Items = items
	} else {
		for _, list := range providerHTMLNodes(document, func(node *html.Node) bool {
			return node.Data == "ol" && providerHTMLAttr(node, "aria-label") != ""
		}) {
			articles := providerHTMLNodes(list, func(node *html.Node) bool {
				return node.Data == "article" && strings.HasPrefix(providerHTMLAttr(node, "aria-labelledby"), "rank-title-")
			})
			if len(articles) == 0 {
				continue
			}
			if len(result.Items) != 0 || len(articles) > 20 {
				return rankingPage{}, errHongguoRankingFormat
			}
			for _, article := range articles {
				item, valid := parseHongguoRankingArticle(article)
				if !valid {
					return rankingPage{}, errHongguoRankingIncomplete
				}
				result.Items = append(result.Items, item)
			}
		}
	}
	if len(result.Items) == 0 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	seen, previous := map[string]bool{}, (page-1)*20
	for _, item := range result.Items {
		if !hongguoNumericID.MatchString(item.Drama.SourceID) || item.Drama.Title == "" || item.Rank <= previous || item.Rank > page*20 || seen[item.Drama.SourceID] {
			return rankingPage{}, errHongguoRankingFormat
		}
		seen[item.Drama.SourceID], previous = true, item.Rank
	}
	if result.HasMore && len(result.Items) != 20 {
		return rankingPage{}, errHongguoRankingIncomplete
	}
	return result, nil
}

func parseHongguoRankingJSONLD(body string, board rankingBoard, page int) ([]rankingItem, bool) {
	for _, match := range rankingJSONLD.FindAllStringSubmatch(body, -1) {
		var value struct {
			Type  string `json:"@type"`
			URL   string `json:"url"`
			Items []struct {
				Position int    `json:"position"`
				Name     string `json:"name"`
				URL      string `json:"url"`
			} `json:"itemListElement"`
		}
		if json.Unmarshal([]byte(match[1]), &value) != nil || value.Type != "ItemList" {
			continue
		}
		actual, valid := hongguoRankingURLPage(value.URL, board)
		if !valid || actual != page || len(value.Items) == 0 || len(value.Items) > 20 {
			continue
		}
		items := make([]rankingItem, 0, len(value.Items))
		for _, row := range value.Items {
			id := hongguoRankingDramaID(row.URL)
			title := strings.TrimSpace(row.Name)
			items = append(items, rankingItem{Rank: row.Position, Drama: Drama{
				ID: providerDramaID(sourceHongguo, id), Source: sourceHongguo, SourceID: id,
				Title: title, Name: title, ChannelName: "红果",
			}})
		}
		return items, true
	}
	return nil, false
}

func parseHongguoRankingArticle(article *html.Node) (rankingItem, bool) {
	label := providerHTMLAttr(article, "aria-labelledby")
	id := strings.TrimPrefix(label, "rank-title-")
	if !hongguoNumericID.MatchString(id) {
		return rankingItem{}, false
	}
	headings := providerHTMLNodes(article, func(node *html.Node) bool {
		return (node.Data == "h2" || node.Data == "h3") && providerHTMLAttr(node, "id") == label
	})
	if len(headings) != 1 || headings[0].Parent == nil || hongguoRankingDramaID(providerHTMLAttr(headings[0].Parent, "href")) != id {
		return rankingItem{}, false
	}
	title := providerHTMLText(headings[0])
	if title == "" || len([]rune(title)) > 200 {
		return rankingItem{}, false
	}
	drama := Drama{ID: providerDramaID(sourceHongguo, id), Source: sourceHongguo, SourceID: id, Title: title, Name: title, ChannelName: "红果"}
	rank := 0
	for _, link := range providerHTMLNodes(article, func(node *html.Node) bool {
		return node.Data == "a" && strings.HasPrefix(providerHTMLAttr(node, "aria-label"), "查看")
	}) {
		if hongguoRankingDramaID(providerHTMLAttr(link, "href")) != id {
			return rankingItem{}, false
		}
		value, err := strconv.Atoi(providerHTMLText(link))
		if err == nil && value > 0 && (rank == 0 || rank == value) {
			rank = value
		}
		for _, image := range providerHTMLNodes(link, func(node *html.Node) bool { return node.Data == "img" }) {
			cover := hongguoCoverAddress(providerHTMLAttr(image, "src"))
			if cover != "" {
				drama.Cover, drama.CoverURL = cover, cover
				break
			}
		}
	}
	for _, node := range providerHTMLNodes(article, func(node *html.Node) bool { return node.Type == html.TextNode }) {
		text := strings.TrimSpace(node.Data)
		if hongguoRankingMetric.MatchString(text) {
			drama.Heat = text
		}
		if strings.HasPrefix(text, "评分") {
			drama.Score = strings.TrimSpace(strings.TrimPrefix(text, "评分"))
		}
	}
	for _, paragraph := range providerHTMLNodes(article, func(node *html.Node) bool { return node.Data == "p" }) {
		var tags []string
		for child := paragraph.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "span" && child.FirstChild != nil && child.FirstChild == child.LastChild && child.FirstChild.Type == html.TextNode {
				value := providerHTMLText(child)
				if value != "" && len([]rune(value)) <= 20 && !strings.Contains(value, "热度") {
					tags = append(tags, value)
				}
			}
		}
		if len(tags) > 0 {
			drama.Tags, drama.CategoryName = tags, tags[0]
		} else if paragraph.FirstChild != nil && paragraph.FirstChild == paragraph.LastChild && paragraph.FirstChild.Type == html.TextNode {
			drama.Desc = providerHTMLText(paragraph)
			drama.Intro = drama.Desc
		}
	}
	return rankingItem{Rank: rank, Drama: drama, Metric: drama.Heat}, rank > 0
}
