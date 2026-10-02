package core

import (
	"regexp"
	"strconv"
	"strings"
)

var huangguoHTMLTag = regexp.MustCompile(`(?is)<(/?)([a-z][a-z0-9:-]*)\b(?:[^<>"']|"[^"]*"|'[^']*')*>`)

var huangguoNonContent = regexp.MustCompile(`(?is)<!--.*?-->|<script\b[^>]*>.*?</script\s*>|<style\b[^>]*>.*?</style\s*>`)

var huangguoTitleFragment = regexp.MustCompile(`(?i)(?:\b(?:class|type|title|aria-label)\s*=|(?:data|ata)-search-input|hg-search(?:__|-))`)

func huangguoElementBlock(raw string, start, end int, tag string) string {
	depth := 1
	for _, match := range huangguoHTMLTag.FindAllStringSubmatchIndex(raw[end:], -1) {
		if !strings.EqualFold(raw[end+match[4]:end+match[5]], tag) {
			continue
		}
		if match[3] > match[2] {
			depth--
		} else if !strings.HasSuffix(strings.TrimSpace(raw[end+match[0]:end+match[1]]), "/>") {
			depth++
		}
		if depth == 0 {
			return raw[start : end+match[1]]
		}
	}
	return raw[start:end]
}

func huangguoClassBlock(raw, className string) string {
	for _, match := range huangguoHTMLTag.FindAllStringSubmatchIndex(raw, -1) {
		if match[3] > match[2] {
			continue
		}
		for _, class := range strings.Fields(extractAttr(raw[match[0]:match[1]], "class")) {
			if class == className {
				return huangguoElementBlock(raw, match[0], match[1], raw[match[4]:match[5]])
			}
		}
	}
	return ""
}

func huangguoClassText(raw, className string) string {
	return cleanText(huangguoClassBlock(raw, className))
}

func huangguoTitleNeedsRepair(title, sourceID string) bool {
	title = strings.TrimSpace(title)
	return title == "" || title == sourceID || title == "短剧" || title == "搜索" ||
		huangguoTitleFragment.MatchString(title) || strings.Contains(title, "搜索记录") && strings.Contains(title, "暂无搜索记录")
}

func firstHuangguoTitle(sourceID string, values ...string) string {
	for _, title := range values {
		if !huangguoTitleNeedsRepair(title, sourceID) {
			return strings.TrimSpace(title)
		}
	}
	return ""
}

func huangguoCardTitle(block, sourceID string) string {
	return firstHuangguoTitle(sourceID,
		extractAttr(block, "data-track-title"), huangguoClassText(block, "hg-drama-card__title"),
		extractAttr(block, "data-search-hot"), huangguoClassText(block, "hg-search-suggest__hot-title"),
		extractAttr(block, "title"), extractAttr(block, "alt"), extractAttr(block, "aria-label"))
}

func huangguoMapEpisodes(row map[string]any) string {
	for _, key := range []string{"episode_count", "episodeCount", "total_episodes", "totalEpisodes", "totalEpisode", "total_episode", "episodes", "total"} {
		if count, err := strconv.Atoi(mapString(row, key)); err == nil && count > 0 {
			return strconv.Itoa(count)
		}
	}
	return ""
}
