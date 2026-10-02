package core

import "encoding/json"

func parseHongguoMergeLoader(body, loaderKey string) (hongguoRankingContent, bool) {
	for _, tag := range rankingScriptTags.FindAllString(body, -1) {
		if extractAttr(tag, "data-fn-name") != "mergeLoaderData" || extractAttr(tag, "data-script-src") != "modern-run-window-fn" {
			continue
		}
		var args []json.RawMessage
		if json.Unmarshal([]byte(extractAttr(tag, "data-fn-args")), &args) != nil || len(args) != 2 {
			continue
		}
		var route string
		if json.Unmarshal(args[0], &route) != nil || route != loaderKey {
			continue
		}
		var fields []struct {
			Key  string            `json:"key"`
			Name string            `json:"routerDataFnName"`
			Args []json.RawMessage `json:"routerDataFnArgs"`
		}
		if json.Unmarshal(args[1], &fields) != nil {
			continue
		}
		for _, field := range fields {
			if field.Key != "content" || field.Name != "p" {
				continue
			}
			var content hongguoRankingContent
			var raw string
			if len(field.Args) != 1 || json.Unmarshal(field.Args[0], &raw) != nil || json.Unmarshal([]byte(raw), &content) != nil {
				return hongguoRankingContent{}, true
			}
			return content, true
		}
	}
	return hongguoRankingContent{}, false
}
