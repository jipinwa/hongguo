package core

import (
	"regexp"
	"strings"
)

var finishedEpisodeRemark = regexp.MustCompile(`(?:全\s*\d+\s*集|\d+\s*集全|已完结|大结局)`)

func releaseStatusFromRemark(remark string) string {
	if strings.Contains(remark, "未完结") || strings.Contains(remark, "更新至") || strings.Contains(remark, "连载") {
		return "ongoing"
	}
	if finishedEpisodeRemark.MatchString(remark) || strings.Contains(remark, "完结") {
		return "finished"
	}
	return "unknown"
}
