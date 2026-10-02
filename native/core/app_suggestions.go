package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

func (engine *nativeEngine) suggestions(ctx context.Context, query string) ([]string, error) {
	if strings.TrimSpace(query) == "" {
		return []string{}, nil
	}
	items, err := engine.downloader.hongguoSearchSuggestions(ctx, query)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names, err
}

func nativeParseSuggestions(body []byte) ([]string, error) {
	var result map[string]json.RawMessage
	if json.Unmarshal(body, &result) != nil {
		return nil, errors.New("搜索联想暂不可用")
	}
	list := result["suggest_list"]
	if len(list) == 0 {
		var data map[string]json.RawMessage
		if json.Unmarshal(result["data"], &data) == nil {
			list = data["suggest_list"]
		}
	}
	if len(list) == 0 || string(list) == "null" {
		return []string{}, nil
	}
	var entries []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(list, &entries) != nil {
		return nil, errors.New("搜索联想暂不可用")
	}
	names, seen := []string{}, map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name != "" && !seen[name] && utf8.RuneCountInString(name) <= 200 {
			names = append(names, name)
			seen[name] = true
			if len(names) == 10 {
				break
			}
		}
	}
	return names, nil
}
