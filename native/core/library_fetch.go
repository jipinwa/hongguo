package core

import "fmt"

func uniqueChapters(chapters []Chapter) []Chapter {
	seen := map[string]bool{}
	var out []Chapter
	for i, ch := range chapters {
		key := ch.ID
		if key == "" {
			key = ch.VideoURL
		}
		if key == "" {
			key = fmt.Sprintf("idx_%d", i)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ch)
	}
	return out
}
