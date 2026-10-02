package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	hongguoSuggestionLimit        = 10
	hongguoSuggestionCacheLimit   = 128
	hongguoSuggestionPendingLimit = 32
	hongguoSuggestionBodyLimit    = 256 * 1024
	hongguoSuggestionTimeout      = 5 * time.Second
	hongguoSuggestionTTL          = 2 * time.Minute
)

type hongguoSearchSuggestion struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type hongguoSuggestionRecord struct {
	Name      string         `json:"name"`
	WordType  string         `json:"word_type"`
	Keyword   any            `json:"keyword"`
	VideoData map[string]any `json:"video_data"`
}

type hongguoSuggestionEntry struct {
	Items     []hongguoSearchSuggestion
	ExpiresAt time.Time
}

type hongguoSuggestionCall struct {
	done  chan struct{}
	items []hongguoSearchSuggestion
	err   error
}

func (d *Downloader) hongguoSearchSuggestions(ctx context.Context, query string) ([]hongguoSearchSuggestion, error) {
	query, err := hongguoSearchKeyword(query)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, hongguoSuggestionTimeout)
	defer cancel()
	client := d.hongguoClient()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client.mu.Lock()
		if cached, found := client.suggestions[query]; found && time.Now().Before(cached.ExpiresAt) {
			items := append([]hongguoSearchSuggestion{}, cached.Items...)
			client.mu.Unlock()
			return items, nil
		}
		if pending := client.suggestPending[query]; pending != nil {
			client.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:

				if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
					continue
				}
				return append([]hongguoSearchSuggestion{}, pending.items...), pending.err
			}
		}
		if len(client.suggestPending) >= hongguoSuggestionPendingLimit {
			client.mu.Unlock()
			return nil, errors.New("红果搜索联想请求较多，请稍后重试")
		}
		pending := &hongguoSuggestionCall{done: make(chan struct{})}
		client.suggestPending[query] = pending
		client.mu.Unlock()

		items, err := d.fetchHongguoSearchSuggestions(ctx, query)
		client.mu.Lock()
		if err == nil {
			now := time.Now()
			oldestKey, oldestTime := "", now.Add(hongguoSuggestionTTL)
			for key, entry := range client.suggestions {
				if !now.Before(entry.ExpiresAt) {
					delete(client.suggestions, key)
				} else if entry.ExpiresAt.Before(oldestTime) {
					oldestKey, oldestTime = key, entry.ExpiresAt
				}
			}
			if len(client.suggestions) >= hongguoSuggestionCacheLimit {
				delete(client.suggestions, oldestKey)
			}
			client.suggestions[query] = hongguoSuggestionEntry{Items: items, ExpiresAt: now.Add(hongguoSuggestionTTL)}
		}
		pending.items, pending.err = items, err
		delete(client.suggestPending, query)
		close(pending.done)
		client.mu.Unlock()
		return append([]hongguoSearchSuggestion{}, items...), err
	}
}

func (d *Downloader) fetchHongguoSearchSuggestions(ctx context.Context, query string) ([]hongguoSearchSuggestion, error) {
	records, err := d.fetchHongguoSuggestionRecords(ctx, query, hongguoSuggestionLimit)
	if err != nil {
		return nil, err
	}
	items := make([]hongguoSearchSuggestion, 0, hongguoSuggestionLimit)
	seen := make(map[string]bool)
	for _, item := range records {
		name, err := hongguoSearchKeyword(item.Name)
		key := strings.ToLower(name)
		if err != nil || seen[key] {
			continue
		}
		seen[key] = true
		kind := item.WordType
		switch kind {
		case "short_play_name", "short_play_category", "common_query", "actor_name", "short_play_actor":
		default:
			kind = ""
		}
		items = append(items, hongguoSearchSuggestion{Name: name, Type: kind})
		if len(items) == hongguoSuggestionLimit {
			break
		}
	}
	return items, nil
}

func (d *Downloader) fetchHongguoSuggestionRecords(ctx context.Context, query string, count int) ([]hongguoSuggestionRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, hongguoSuggestionTimeout)
	defer cancel()
	params := url.Values{"app_id": {"8662"}, "query": {query}, "count": {strconv.Itoa(count)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, hongguoBaseURL+"/incent_resource/suggestion?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", hongguoBaseURL+"/")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	response, err := d.doCatalogRequestWithTimeout(request, hongguoSuggestionTimeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, hongguoSuggestionBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > hongguoSuggestionBodyLimit {
		return nil, errors.New("红果搜索联想响应过大")
	}
	if response.StatusCode != http.StatusOK || catalogResponseBlockReason(response, body) != "" {
		return nil, d.catalogResponseError(request, response, body)
	}
	var result struct {
		Items []hongguoSuggestionRecord `json:"suggest_list"`
		Data  struct {
			Items []hongguoSuggestionRecord `json:"suggest_list"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("红果搜索联想未返回有效数据")
	}
	if result.Items == nil {
		result.Items = result.Data.Items
	}
	if result.Items == nil {
		return nil, errors.New("红果搜索联想未返回有效数据")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("红果搜索联想未返回有效数据")
	}
	return result.Items, nil
}
