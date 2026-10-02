package core

import (
	"encoding/json"
	"strconv"
	"strings"
)

type Drama struct {
	VIP               *bool              `json:"vip,omitempty"`
	ID                string             `json:"id"`
	Source            string             `json:"source,omitempty"`
	SourceID          string             `json:"sourceId,omitempty"`
	Title             string             `json:"title"`
	Name              string             `json:"name"`
	Desc              string             `json:"desc"`
	Intro             string             `json:"intro"`
	Cover             any                `json:"cover"`
	CoverURL          any                `json:"coverUrl"`
	CoverURLSnake     any                `json:"cover_url"`
	Image             any                `json:"image"`
	ImageURL          any                `json:"imageUrl"`
	ImageURLSnake     any                `json:"image_url"`
	Img               any                `json:"img"`
	Pic               any                `json:"pic"`
	Picture           any                `json:"picture"`
	Poster            any                `json:"poster"`
	Thumb             any                `json:"thumb"`
	Thumbnail         any                `json:"thumbnail"`
	TotalEpisode      any                `json:"totalEpisode"`
	TotalEpisodeSnake any                `json:"total_episode"`
	ChapterCount      any                `json:"chapterCount"`
	ChapterCountSnake any                `json:"chapter_count"`
	EpisodeCount      any                `json:"episodeCount"`
	EpisodeCountSnake any                `json:"episode_count"`
	Total             any                `json:"total"`
	Episodes          any                `json:"episodes"`
	Duration          any                `json:"duration"`
	ChannelName       string             `json:"channelName"`
	Category          string             `json:"category"`
	CategoryName      string             `json:"categoryName"`
	CategoryNameSnake string             `json:"category_name"`
	TypeName          string             `json:"typeName"`
	TypeNameSnake     string             `json:"type_name"`
	SortName          string             `json:"sortName"`
	SortNameSnake     string             `json:"sort_name"`
	Remark            string             `json:"remark,omitempty"`
	Score             string             `json:"score,omitempty"`
	Views             string             `json:"views,omitempty"`
	Heat              string             `json:"heat,omitempty"`
	OnlineDate        string             `json:"onlineDate,omitempty"`
	Tags              []string           `json:"tags,omitempty"`
	ReleaseStatus     string             `json:"releaseStatus,omitempty"`
	SortMetadata      *sortMetadataState `json:"sortMetadata,omitempty"`
}

func (d Drama) DisplayTitle() string {
	if strings.TrimSpace(d.Title) != "" {
		return d.Title
	}
	if strings.TrimSpace(d.Name) != "" {
		return d.Name
	}
	return "短剧"
}

type Chapter struct {
	VIP            bool            `json:"vip,omitempty"`
	ID             string          `json:"id"`
	Source         string          `json:"source,omitempty"`
	Title          string          `json:"title"`
	VideoURL       string          `json:"videoUrl"`
	CurrentEpisode json.RawMessage `json:"currentEpisode"`
	MediaSize      int64           `json:"mediaSize"`
	PageURL        string          `json:"pageUrl,omitempty"`
	Referer        string          `json:"referer,omitempty"`
}

func (c Chapter) EpisodeString(fallback int) string {
	if len(c.CurrentEpisode) > 0 && string(c.CurrentEpisode) != "null" {
		var s string
		if err := json.Unmarshal(c.CurrentEpisode, &s); err == nil && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		var n int
		if err := json.Unmarshal(c.CurrentEpisode, &n); err == nil && n > 0 {
			return strconv.Itoa(n)
		}
		var f float64
		if err := json.Unmarshal(c.CurrentEpisode, &f); err == nil && f > 0 {
			return strconv.Itoa(int(f))
		}
	}
	return strconv.Itoa(fallback)
}

type Task struct {
	DramaID         string  `json:"dramaId"`
	DramaTitle      string  `json:"dramaTitle"`
	Chapter         Chapter `json:"chapter"`
	Index           int     `json:"index"`
	Total           int     `json:"total"`
	OutPath         string  `json:"outPath"`
	ReleaseStatus   string  `json:"releaseStatus,omitempty"`
	DownloadQuality int     `json:"downloadQuality,omitempty"`
}
