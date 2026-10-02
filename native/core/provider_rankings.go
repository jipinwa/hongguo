package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const rankingCacheTTL = 5 * time.Minute

type rankingBoard struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	Description string `json:"description"`
	path        string
	upstreamKey string
}

var rankingBoards = []rankingBoard{
	{ID: "hongguo-hot", Source: sourceHongguo, Name: "总热播榜", Description: "红果观看、互动等综合热度；每日更新。", path: "hot-drama", upstreamKey: "hongguo"},
	{ID: "hongguo-real", Source: sourceHongguo, Name: "真人剧榜", Description: "红果真人剧热播榜；每日更新。", path: "hot-real-drama", upstreamKey: "real"},
	{ID: "hongguo-comic", Source: sourceHongguo, Name: "漫剧榜", Description: "红果漫剧热播榜；每日更新。", path: "hot-comic-drama", upstreamKey: "comic"},
	{ID: "hongguo-ai", Source: sourceHongguo, Name: "AI剧榜", Description: "红果 AI 剧热播榜；每日更新。", path: "hot-ai-drama", upstreamKey: "ai"},
	{ID: "huangdou-all", Source: sourceHuangdou, Name: "总榜", Description: "黄豆短剧总榜，保留站点返回的顺序。", upstreamKey: "all"},
	{ID: "huangdou-mogai", Source: sourceHuangdou, Name: "魔改榜", Description: "黄豆站点魔改榜。", upstreamKey: "mogai"},
	{ID: "huangdou-search", Source: sourceHuangdou, Name: "搜索榜", Description: "黄豆站点搜索榜。", upstreamKey: "search"},
	{ID: "huangdou-favorite", Source: sourceHuangdou, Name: "收藏榜", Description: "黄豆站点收藏榜。", upstreamKey: "favorite"},
	{ID: "huangdou-finish", Source: sourceHuangdou, Name: "完结榜", Description: "黄豆站点完结榜。", upstreamKey: "finish"},
	{ID: "huangguo-hot", Source: sourceHuangguoAI, Name: "热播榜", Description: "黄果站点热播 TOP 20；每日更新。", path: "hot"},
	{ID: "huangguo-recommend", Source: sourceHuangguoAI, Name: "推荐榜", Description: "黄果站点推荐 TOP 20。", path: "recommend"},
	{ID: "huangguo-potential", Source: sourceHuangguoAI, Name: "潜力榜", Description: "黄果站点潜力 TOP 20。", path: "potential"},
	{ID: "huangju-hot", Source: sourceHuangju, Name: "热门榜", Description: "剧果目录热门顺序，按源站返回顺序展示。"},
	{ID: "huangju-new", Source: sourceHuangju, Name: "最新榜", Description: "剧果最新目录顺序，按源站返回顺序展示。", path: huangjuNewestCategory},
	{ID: "yeguo-recommend", Source: sourceYeguo, Name: "目录榜", Description: "野果默认目录顺序，按源站返回顺序展示。"},
	{ID: "dsd-catalog", Source: sourceDSD, Name: "目录榜", Description: "帝果分类目录聚合顺序，按源站返回顺序展示。"},
}

type rankingItem struct {
	Rank   int    `json:"rank"`
	Drama  Drama  `json:"drama"`
	Metric string `json:"metric,omitempty"`
}

type rankingPage struct {
	BoardID     string        `json:"boardId"`
	Page        int           `json:"page"`
	Items       []rankingItem `json:"items"`
	HasMore     bool          `json:"hasMore"`
	TotalPages  int           `json:"totalPages,omitempty"`
	UpdatedText string        `json:"updatedText,omitempty"`
	FetchedAt   time.Time     `json:"fetchedAt"`
	Stale       bool          `json:"stale,omitempty"`
	Warning     string        `json:"warning,omitempty"`
}

type rankingCall struct {
	done chan struct{}
	page rankingPage
	err  error
}

type rankingCache struct {
	mu      sync.Mutex
	pages   map[string]rankingPage
	pending map[string]*rankingCall
}

func findRankingBoard(id string) (rankingBoard, bool) {
	for _, board := range rankingBoards {
		if board.ID == id {
			return board, true
		}
	}
	return rankingBoard{}, false
}

func cloneRankingPage(page rankingPage) rankingPage {
	page.Items = append([]rankingItem{}, page.Items...)
	return page
}

func singlePageRankingBoard(board rankingBoard) bool {
	return board.Source == sourceHuangguoAI || board.Source == sourceDSD
}

func rankingDramaWithoutImages(drama Drama) Drama {
	drama.Cover, drama.CoverURL, drama.CoverURLSnake = nil, nil, nil
	drama.Image, drama.ImageURL, drama.ImageURLSnake = nil, nil, nil
	drama.Img, drama.Pic, drama.Picture = nil, nil, nil
	drama.Poster, drama.Thumb, drama.Thumbnail = nil, nil, nil
	return drama
}

func (d *Downloader) loadRankingPage(ctx context.Context, board rankingBoard, page int, refresh bool) (rankingPage, error) {
	key := board.ID + ":" + strconv.Itoa(page)
	cache := &d.rankings
	cache.mu.Lock()
	if cache.pages == nil {
		cache.pages = map[string]rankingPage{}
		cache.pending = map[string]*rankingCall{}
	}
	cached, exists := cache.pages[key]
	if exists && !refresh && time.Since(cached.FetchedAt) >= 0 && time.Since(cached.FetchedAt) < rankingCacheTTL {
		cache.mu.Unlock()
		return cloneRankingPage(cached), nil
	}
	if pending := cache.pending[key]; pending != nil {
		cache.mu.Unlock()
		select {
		case <-ctx.Done():
			return rankingPage{}, ctx.Err()
		case <-pending.done:
			if (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return d.loadRankingPage(ctx, board, page, refresh)
			}
			return cloneRankingPage(pending.page), pending.err
		}
	}
	pending := &rankingCall{done: make(chan struct{})}
	cache.pending[key] = pending
	cache.mu.Unlock()

	result, err := d.fetchRankingPage(ctx, board, page)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err == nil {
		result.BoardID, result.Page, result.FetchedAt = board.ID, page, time.Now()
		if refresh && page == 1 {
			for cachedKey, old := range cache.pages {
				if old.BoardID == board.ID {
					delete(cache.pages, cachedKey)
				}
			}
		}
		if len(cache.pages) >= 128 {
			oldestKey, oldest := "", time.Now()
			for cachedKey, old := range cache.pages {
				if old.FetchedAt.Before(oldest) {
					oldestKey, oldest = cachedKey, old.FetchedAt
				}
			}
			delete(cache.pages, oldestKey)
		}
		cache.pages[key] = cloneRankingPage(result)
		d.writeRankingCacheLocked()
	} else if exists && ctx.Err() == nil && time.Since(cached.FetchedAt) >= 0 && time.Since(cached.FetchedAt) < 24*time.Hour {
		result = cloneRankingPage(cached)
		result.Stale = true
		result.Warning = "站点暂不可用，显示上次取得的榜单，请稍后刷新。"
		err = nil
	}
	pending.page, pending.err = cloneRankingPage(result), err
	delete(cache.pending, key)
	close(pending.done)
	return result, err
}

func (d *Downloader) fetchRankingPage(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	switch board.Source {
	case sourceHongguo:
		return d.fetchHongguoRankingPage(ctx, board, page)
	case sourceHuangdou:
		var decoded any
		err := newHuangdouAPIClient(d).call(ctx, "/drama/rank", map[string]any{"tab": board.upstreamKey, "page": strconv.Itoa(page)}, &decoded)
		if err != nil {
			return rankingPage{}, err
		}
		return parseHuangdouRanking(decoded, page)
	case sourceHuangguoAI:
		if page != 1 {
			return rankingPage{}, errors.New("该榜单只有一页")
		}
		pageURL := d.providerBaseURL(sourceHuangguoAI) + "/ranks/" + board.path + "/"
		body, err := d.fetchProviderText(ctx, pageURL, d.providerBaseURL(sourceHuangguoAI)+"/")
		if err != nil {
			return rankingPage{}, err
		}
		return parseHuangguoRanking(body, board)
	case sourceHuangju, sourceYeguo, sourceDSD:
		return d.fetchCatalogRankingPage(ctx, board, page)
	default:
		return rankingPage{}, fmt.Errorf("不支持的榜单站源 %s", board.Source)
	}
}

func (d *Downloader) fetchCatalogRankingPage(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	if singlePageRankingBoard(board) && page != 1 {
		return rankingPage{}, errors.New("该榜单只有一页")
	}
	category := board.path
	if board.Source == sourceYeguo && category == "recommend" {
		category = ""
		categories, err := d.fetchYeguoCategories(ctx)
		if err == nil {
			for _, item := range categories {
				if strings.HasPrefix(item.ID, "recommend:") {
					category = item.ID
					break
				}
			}
		}
	}
	var items []Drama
	var more bool
	var err error
	switch board.Source {
	case sourceHuangju:
		items, more, err = d.fetchHuangjuCatalogPage(ctx, page, category, "")
	case sourceYeguo:
		items, more, err = d.fetchYeguoCatalogPage(ctx, page, category, "")
	case sourceDSD:
		items, more, err = d.fetchDSDCatalogPage(ctx, page, category, "")
	}
	if err != nil {
		return rankingPage{}, err
	}
	if board.Source == sourceDSD {
		more = false
	}
	result := rankingPage{Items: make([]rankingItem, 0, len(items)), HasMore: more}
	for index, drama := range items {
		result.Items = append(result.Items, rankingItem{Rank: (page-1)*20 + index + 1, Drama: rankingDramaWithoutImages(drama)})
	}
	return result, nil
}
