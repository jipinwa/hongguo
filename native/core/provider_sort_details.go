package core

import (
	"strconv"
	"strings"
	"time"
)

func providerReleaseDate(value string) string {
	value = strings.TrimSpace(value)
	if stamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		value = stamp.In(providerChinaTime).Format("2006-01-02")
	}
	date := normalizeDate(value)
	if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Year() < 2000 || parsed.Year() > 2100 {
		return ""
	}
	return date
}

func providerTimestampDate(value string) string {
	stamp, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || stamp <= 0 {
		return ""
	}
	if stamp > 100_000_000_000 {
		stamp /= 1000
	}
	date := time.Unix(stamp, 0).In(providerChinaTime)
	if date.Year() < 2000 || date.Year() > 2100 {
		return ""
	}
	return date.Format("2006-01-02")
}
