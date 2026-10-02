package core

import (
	"math"
	"strconv"
	"strings"
	"time"
)

var providerChinaTime = time.FixedZone("CST", 8*60*60)

func hongguoHeat(row map[string]any) string {
	hot := nestedMap(row, "hot_score_data")

	if score := firstNonEmpty(mapString(hot, "score"), mapString(row, "hot_score")); score != "" {
		value, err := strconv.ParseFloat(score, 64)
		if err == nil && value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value) {
			return score
		}
	}
	return mapString(hot, "text")
}

func hongguoOnlineDate(row map[string]any, now time.Time) string {
	if date := providerTimestampDate(mapString(row, "first_visible_time")); date != "" {
		return date
	}
	for _, value := range anyList(row["sub_title_list"]) {
		label := mapString(nestedMap(value), "content")
		day := now.In(providerChinaTime)
		switch label {
		case "今日上新":
			return day.Format("2006-01-02")
		case "昨日上新":
			return day.AddDate(0, 0, -1).Format("2006-01-02")
		default:
			if strings.HasSuffix(label, "上新") {
				date := normalizeDate(label)
				if _, err := time.Parse("2006-01-02", date); err == nil {
					return date
				}
			}
		}
	}
	return ""
}
