package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const (
	dsdBaseURL   = "https://www.dsd.com.se"
	dsdUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var dsdBackgroundURL = regexp.MustCompile(`(?i)url\(\s*["']?([^"')]+)`)

type dsdPageLimit struct {
	pages     int
	updatedAt time.Time
}

type dsdCatalogState struct {
	mu         sync.Mutex
	categories []nativeCategory
	updatedAt  time.Time
	limits     map[string]dsdPageLimit
}

func dsdRouteParameters(pageURL, reference string) (string, map[string]string, bool) {
	page, err := url.Parse(pageURL)
	if err != nil || reference == "" {
		return "", nil, false
	}
	address, err := url.Parse(reference)
	if err != nil {
		return "", nil, false
	}
	address = page.ResolveReference(address)
	if address.User != nil || providerMediaOrigin(address) != providerMediaOrigin(page) {
		return "", nil, false
	}
	path := strings.TrimPrefix(address.Path, "/index.php")
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".html"), "/"), "/")
	if len(parts) < 2 || parts[0] != "vod" || len(parts)%2 != 0 {
		return "", nil, false
	}
	switch parts[1] {
	case "type", "show", "search", "play", "detail":
	default:
		return "", nil, false
	}
	parameters := map[string]string{}
	for index := 2; index+1 < len(parts); index += 2 {
		parameters[parts[index]] = parts[index+1]
	}
	for key, values := range address.Query() {
		if len(values) == 1 && parameters[key] == "" {
			parameters[key] = values[0]
		}
	}
	return parts[1], parameters, true
}

func parseDSDCategories(document *html.Node, pageURL string) ([]nativeCategory, error) {
	var categories []nativeCategory
	seen := map[string]bool{}
	for _, anchor := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Data == "a" }) {
		action, values, valid := dsdRouteParameters(pageURL, providerHTMLAttr(anchor, "href"))
		id, name := values["id"], providerHTMLText(anchor)
		if !valid || action != "type" || !webProviderNumericID.MatchString(id) || seen[id] || name == "" || len([]rune(name)) > 48 {
			continue
		}
		seen[id] = true
		categories = append(categories, nativeCategory{ID: id, Name: name})
	}
	if len(categories) == 0 || len(categories) > 32 {
		return nil, errors.New("帝果未返回有效的内容分类")
	}
	return categories, nil
}

func (d *Downloader) fetchDSDCategories(ctx context.Context, force bool) ([]nativeCategory, error) {
	state := &d.dsdCatalog
	state.mu.Lock()
	if len(state.categories) > 0 && !force && time.Since(state.updatedAt) < time.Hour {
		categories := append([]nativeCategory(nil), state.categories...)
		state.mu.Unlock()
		return categories, nil
	}
	state.mu.Unlock()
	site := d.providerBaseURL(sourceDSD)
	document, address, err := d.fetchProviderPage(ctx, site+"/", site+"/", dsdUserAgent)
	if err != nil {
		return nil, err
	}
	categories, err := parseDSDCategories(document, address)
	if err != nil {
		return nil, err
	}
	state.mu.Lock()
	state.categories, state.updatedAt = append([]nativeCategory(nil), categories...), time.Now()
	if force {
		state.limits = nil
	}
	state.mu.Unlock()
	return categories, nil
}

func dsdCardCover(card *html.Node, pageURL string) string {
	images := providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "img" })
	for _, image := range images {
		for _, attribute := range []string{"data-src", "data-original"} {
			if address := providerCoverAddress(providerHTMLAttr(image, attribute), pageURL); address != "" {
				return address
			}
		}
	}
	for _, node := range providerHTMLNodes(card, func(node *html.Node) bool {
		return node == card || providerHTMLClass(node, "img-bg") || providerHTMLClass(node, "surface-img") ||
			providerHTMLClass(node, "openVip") || providerHTMLClass(node, "video-before-ad")
	}) {
		if match := dsdBackgroundURL.FindStringSubmatch(providerHTMLAttr(node, "style")); len(match) > 1 {
			if address := providerCoverAddress(strings.TrimSpace(match[1]), pageURL); address != "" {
				return address
			}
		}
	}
	for _, image := range images {
		raw := providerHTMLAttr(image, "src")
		if strings.HasPrefix(raw, "/video") || strings.Contains(raw, "/upload") {
			if address := providerCoverAddress(raw, pageURL); address != "" {
				return address
			}
		}
	}
	return ""
}

func parseDSDCards(document *html.Node, pageURL, category string) ([]Drama, error) {
	items := []Drama{}
	seen := map[string]bool{}
	for _, card := range providerHTMLNodes(document, func(node *html.Node) bool {
		return node.Data == "a" && (providerHTMLClass(node, "video-item") || providerHTMLClass(node, "video-surface"))
	}) {
		action, values, valid := dsdRouteParameters(pageURL, providerHTMLAttr(card, "href"))
		id := values["id"]
		if !valid || action != "play" && action != "detail" || !webProviderNumericID.MatchString(id) || seen[id] {
			continue
		}
		title := firstNonEmpty(providerHTMLText(providerHTMLFirstClass(card, "video-desc", "surface-title", "video-title", "title")), providerHTMLAttr(card, "title"))
		if title == "" {
			for _, image := range providerHTMLNodes(card, func(node *html.Node) bool { return node.Data == "img" }) {
				if title = strings.TrimSpace(providerHTMLAttr(image, "alt")); title != "" {
					break
				}
			}
		}
		if title == "" {
			return nil, errors.New("帝果目录缺少视频名称")
		}
		seen[id] = true
		cover := dsdCardCover(card, pageURL)
		views := strings.TrimSuffix(providerHTMLText(providerHTMLFirstClass(card, "video-item-tag-hits", "counts")), "次")
		drama := Drama{ID: providerDramaID(sourceDSD, id), Source: sourceDSD, SourceID: id,
			Title: truncate(title, 1024), Name: truncate(title, 1024), Cover: cover, CoverURL: cover,
			CategoryName: firstNonEmpty(category, "视频"), ChannelName: "帝果", Views: views}
		if providerHTMLFirstClass(card, "video-item-tag-is-vip", "vip-tag") != nil {
			vip := true
			drama.VIP = &vip
		}
		items = append(items, drama)
		if len(items) > 500 {
			return nil, errors.New("帝果目录条目过多")
		}
	}
	return items, nil
}

func dsdPagination(document *html.Node, pageURL, category, query string, page int) (int, error) {
	pagination := providerHTMLFirstClass(document, "el-pagination", "pagination", "mac_pages")
	if pagination == nil {
		if page != 1 {
			return 0, errors.New("帝果未返回请求页码，请重试")
		}
		return 1, nil
	}
	activePage := 0
	for _, node := range providerHTMLNodes(pagination, func(node *html.Node) bool {
		return providerHTMLClass(node, "active") && providerHTMLClass(node, "number") || providerHTMLClass(node, "page-current")
	}) {
		if current, valid := webProviderInteger(providerHTMLText(node), 1000000); valid {
			activePage = current
			break
		}
	}
	if activePage != page {
		return 0, errors.New("帝果返回的页码与请求不符")
	}
	pages := page
	for _, anchor := range providerHTMLNodes(pagination, func(node *html.Node) bool { return node.Data == "a" }) {
		action, parameters, valid := dsdRouteParameters(pageURL, providerHTMLAttr(anchor, "href"))
		if !valid {
			continue
		}
		if query != "" {
			if action != "search" || parameters["wd"] != query {
				continue
			}
		} else if (action != "type" && action != "show") || parameters["id"] != category {
			continue
		}
		if value, valid := webProviderInteger(parameters["page"], 1000000); valid {
			pages = max(pages, value)
		}
	}
	return pages, nil
}

func (d *Downloader) dsdPageLimit(category string) (int, bool) {
	state := &d.dsdCatalog
	state.mu.Lock()
	defer state.mu.Unlock()
	limit, found := state.limits[category]
	return limit.pages, found && time.Since(limit.updatedAt) < 15*time.Minute
}

func (d *Downloader) fetchDSDPage(ctx context.Context, page int, category nativeCategory, query string) ([]Drama, bool, error) {
	if query == "" && page > 1 {
		limit, known := d.dsdPageLimit(category.ID)
		if !known {
			if _, _, err := d.fetchDSDPage(ctx, 1, category, ""); err != nil {
				return nil, false, err
			}
			limit, known = d.dsdPageLimit(category.ID)
		}
		if known && page > limit {
			return []Drama{}, false, nil
		}
	}
	site := d.providerBaseURL(sourceDSD)
	address := fmt.Sprintf("%s/index.php/vod/type/id/%s/page/%d.html", site, category.ID, page)
	if query != "" {
		address = site + "/index.php/vod/search.html?" + url.Values{"wd": {query}, "page": {strconv.Itoa(page)}}.Encode()
	}
	document, address, err := d.fetchProviderPage(ctx, address, site+"/", dsdUserAgent)
	if err != nil {
		return nil, false, err
	}
	items, err := parseDSDCards(document, address, category.Name)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 && providerHTMLFirstClass(document, "video-contaner", "nvyou-wrap", "van-list", "search-page", "lists") == nil {
		return nil, false, errors.New("帝果未返回内容列表，页面可能已变化")
	}
	pages, err := dsdPagination(document, address, category.ID, query, page)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 && pages > page {
		return nil, false, errors.New("帝果未返回应有的视频条目，请重试")
	}
	if query == "" {
		state := &d.dsdCatalog
		state.mu.Lock()
		if state.limits == nil {
			state.limits = map[string]dsdPageLimit{}
		}
		state.limits[category.ID] = dsdPageLimit{pages: pages, updatedAt: time.Now()}
		state.mu.Unlock()
	}
	return items, pages > page, nil
}

func (d *Downloader) fetchDSDCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 1000000 || len(query) > 1024 || category != "" && !webProviderNumericID.MatchString(category) {
		return nil, false, errors.New("帝果目录查询参数无效")
	}
	if query != "" {
		return d.fetchDSDPage(ctx, page, nativeCategory{}, query)
	}
	categories, err := d.fetchDSDCategories(ctx, false)
	if err != nil {
		return nil, false, err
	}
	if category != "" {
		for _, item := range categories {
			if item.ID == category {
				return d.fetchDSDPage(ctx, page, item, "")
			}
		}
		return nil, false, errors.New("帝果分类已变化，请刷新分类")
	}
	type result struct {
		items []Drama
		more  bool
		err   error
	}
	results := make([]result, len(categories))
	slots := make(chan struct{}, 2)
	var group sync.WaitGroup
	for index, category := range categories {
		group.Add(1)
		go func(index int, category nativeCategory) {
			defer group.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results[index].err = ctx.Err()
				return
			}
			results[index].items, results[index].more, results[index].err = d.fetchDSDPage(ctx, page, category, "")
		}(index, category)
	}
	group.Wait()
	items := []Drama{}
	seen := map[string]bool{}
	var failures []error
	hasMore, count := false, 0
	for index, result := range results {
		hasMore = hasMore || result.more || result.err != nil
		count = max(count, len(result.items))
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s：%w", categories[index].Name, result.err))
		}
	}
	for index := 0; index < count; index++ {
		for _, result := range results {
			if index < len(result.items) && !seen[result.items[index].ID] {
				item := result.items[index]
				seen[item.ID] = true
				items = append(items, item)
			}
		}
	}
	return items, hasMore, errors.Join(failures...)
}
