package core

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	hongguoSearchSeasonLimit   = 200
	hongguoSearchSeasonQueries = 32
	hongguoSearchSeasonTimeout = 25 * time.Second
)

var hongguoSearchSeasonSuffix = regexp.MustCompile(`第\s*([0-9零〇一二两兩三四五六七八九十百]+)\s*([季部])[\p{P}\s]*$`)

type hongguoSearchSeries struct {
	title   string
	key     string
	unit    string
	maximum int
	known   map[int]bool
	labels  map[int]string
}

func hongguoSearchSeason(title string) (string, int, string, string) {
	title = norm.NFKC.String(strings.TrimSpace(title))
	match := hongguoSearchSeasonSuffix.FindStringSubmatchIndex(title)
	if match == nil {
		return "", 0, "", ""
	}
	base := strings.TrimRightFunc(title[:match[0]], func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
	label, unit := title[match[2]:match[3]], title[match[4]:match[5]]
	number, err := strconv.Atoi(label)
	if err != nil {
		number = 0
		digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '兩': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
		if !strings.ContainsAny(label, "十百") {
			for _, r := range label {
				value, ok := digits[r]
				if !ok {
					return "", 0, "", ""
				}
				number = number*10 + value
				if number > hongguoSearchSeasonLimit {
					return "", 0, "", ""
				}
			}
		} else {
			digit, previous := 0, 1000
			for _, r := range label {
				if value, ok := digits[r]; ok {
					digit = digit*10 + value
					continue
				}
				value := 10
				if r == '百' {
					value = 100
				}
				if value >= previous || digit > 9 {
					return "", 0, "", ""
				}
				number += max(digit, 1) * value
				digit, previous = 0, value
			}
			number += digit
		}
	}
	if base == "" || number < 1 || number > hongguoSearchSeasonLimit {
		return "", 0, "", ""
	}
	return base, number, unit, label
}

func hongguoSearchChineseSeason(number int) string {
	digits := []rune("零一二三四五六七八九")
	if number < 10 {
		return string(digits[number])
	}
	if number < 100 {
		prefix := ""
		if number >= 20 {
			prefix = string(digits[number/10])
		}
		prefix += "十"
		if number%10 != 0 {
			prefix += string(digits[number%10])
		}
		return prefix
	}
	prefix, rest := string(digits[number/100])+"百", number%100
	if rest == 0 {
		return prefix
	}
	if rest < 10 {
		prefix += "零"
	} else if rest < 20 {
		prefix += "一"
	}
	return prefix + hongguoSearchChineseSeason(rest)
}

func (series *hongguoSearchSeries) add(drama Drama) bool {
	base, season, unit, label := hongguoSearchSeason(drama.DisplayTitle())
	if season == 0 && hongguoSearchText(drama.DisplayTitle()) == series.key {
		season = 1
	} else if season == 0 || unit != series.unit || hongguoSearchText(base) != series.key {
		return false
	}
	series.known[season] = true
	series.maximum = max(series.maximum, season)
	if label != "" {
		series.labels[season] = label
	}
	return true
}

func hongguoSearchSeriesGroups(keyword string, dramas []Drama) []*hongguoSearchSeries {
	if _, season, _, _ := hongguoSearchSeason(keyword); season > 0 {
		return nil
	}
	query := hongguoSearchText(keyword)
	var groups []*hongguoSearchSeries
	byKey := map[string]*hongguoSearchSeries{}
	for _, drama := range dramas {
		base, season, unit, _ := hongguoSearchSeason(drama.DisplayTitle())
		key := hongguoSearchText(base)
		if season == 0 || !strings.Contains(key, query) {
			continue
		}
		groupKey := key + ":" + unit
		if byKey[groupKey] == nil {
			series := &hongguoSearchSeries{title: base, key: key, unit: unit, known: map[int]bool{}, labels: map[int]string{}}
			byKey[groupKey] = series
			groups = append(groups, series)
		}
	}
	var incomplete []*hongguoSearchSeries
	for _, group := range groups {
		for _, drama := range dramas {
			group.add(drama)
		}
		if len(group.known) >= 2 && len(group.known) < group.maximum {
			incomplete = append(incomplete, group)
		}
	}
	sort.SliceStable(incomplete, func(left, right int) bool {
		l, r := incomplete[left], incomplete[right]
		lr, rr := hongguoTitleSearchRank(l.title, query), hongguoTitleSearchRank(r.title, query)
		if lr != rr {
			return lr < rr
		}
		return len(l.known) > len(r.known)
	})
	return incomplete
}

func (series *hongguoSearchSeries) nextQuery(attempted map[string]bool) string {
	for season := 1; season <= series.maximum; season++ {
		if series.known[season] {
			continue
		}
		nearest, distance := 0, hongguoSearchSeasonLimit+1
		for known := 1; known <= series.maximum; known++ {
			delta := max(known-season, season-known)
			if series.labels[known] != "" && delta < distance {
				nearest, distance = known, delta
			}
		}
		labels := []string{hongguoSearchChineseSeason(season), strconv.Itoa(season)}
		if _, err := strconv.Atoi(series.labels[nearest]); err == nil {
			labels[0], labels[1] = labels[1], labels[0]
		}
		var queries []string
		if season == 1 {
			queries = append(queries, series.title)
		}
		for _, label := range labels {
			queries = append(queries, series.title+"第"+label+series.unit)
		}
		for _, query := range queries {
			if _, err := hongguoSearchKeyword(query); err == nil && !attempted[query] {
				return query
			}
		}
	}
	return ""
}

func (downloader *Downloader) completeHongguoSearchSeasons(ctx context.Context, keyword string, entry *hongguoSearchEntry) bool {
	groups := hongguoSearchSeriesGroups(keyword, entry.Dramas)
	if len(groups) == 0 {
		return false
	}
	budget := hongguoSearchSeasonTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-100*time.Millisecond)
	}
	if budget <= 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	attempted := map[string]bool{keyword: true}
	requests := 0
	for _, group := range groups {
		for requests < hongguoSearchSeasonQueries {
			query := group.nextQuery(attempted)
			if query == "" {
				break
			}
			attempted[query] = true
			requests++
			names, err := downloader.fetchHongguoSearchNames(ctx, query)
			if err != nil {
				return true
			}
			var matches []Drama
			for _, drama := range names {
				if group.add(drama) {
					matches = append(matches, drama)
				}
			}
			if len(matches) > 0 {
				entry.Dramas = mergeHongguoSearchDramas(entry.Dramas, matches)
				reportHongguoSearchProgress(ctx, *entry)
			}
		}
	}
	for _, group := range groups {
		if len(group.known) < group.maximum {
			return true
		}
	}
	return false
}
