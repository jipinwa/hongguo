package core

import (
	"fmt"
	"strconv"
	"strings"
)

func huangdouVIPFlag(item map[string]any) *bool {
	var known *bool
	for _, key := range []string{"is_vip", "isVip"} {
		value := strings.ToLower(mapString(item, key))
		if value == "true" || value == "1" || value == "y" {
			result := true
			return &result
		}
		if value == "false" || value == "0" || value == "n" {
			result := false
			known = &result
		}
	}
	switch strings.ToLower(mapString(item, "pay_type")) {
	case "vip":
		result := true
		return &result
	case "free":
		result := false
		known = &result
	}
	for _, episode := range huangdouVIPEpisodes(item) {
		if episode > 0 {
			result := true
			return &result
		}
	}
	for _, episode := range huangdouList(item["episodes"]) {
		if strings.EqualFold(mapString(episode, "type"), "vip") {
			result := true
			return &result
		}
	}
	return known
}

func huangdouVIPEpisodes(item map[string]any) []int {
	var result []int
	if values, ok := item["vip_episodes"].([]any); ok {
		for _, value := range values {
			if index, err := strconv.Atoi(fmt.Sprint(value)); err == nil && index > 0 && index <= 10000 {
				result = append(result, index)
			}
		}
	}
	return result
}

func huangdouEpisodeVIP(detail, episode map[string]any, seq int) bool {
	switch strings.ToLower(mapString(episode, "type")) {
	case "vip":
		return true
	case "free":
		return false
	}
	for _, index := range huangdouVIPEpisodes(detail) {
		if index == seq {
			return true
		}
	}
	if flag := huangdouVIPFlag(episode); flag != nil {
		return *flag
	}
	if strings.EqualFold(mapString(detail, "pay_type"), "vip") {
		free, err := strconv.Atoi(mapString(detail, "free_episodes"))
		return err == nil && free >= 0 && seq > free
	}
	return false
}
