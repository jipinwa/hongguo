package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type duanjuRequest struct {
	Source  string
	Method  string
	Address string
	Referer string
	Agent   string
	Headers map[string]string
	Body    []byte
	Form    url.Values
	Timeout time.Duration
}

func (d *Downloader) duanjuDo(ctx context.Context, request duanjuRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	address := strings.TrimSpace(request.Address)
	if address == "" {
		return nil, errors.New("站源地址为空")
	}
	body := request.Body
	if len(request.Form) > 0 && len(body) == 0 {
		body = []byte(request.Form.Encode())
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return nil, err
	}
	agent := firstNonEmpty(request.Agent, duanjuUserAgent)
	httpRequest.Header.Set("User-Agent", agent)
	httpRequest.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	httpRequest.Header.Set("Accept", "*/*")
	if referer := strings.TrimSpace(request.Referer); referer != "" {
		httpRequest.Header.Set("Referer", referer)
	}
	if len(request.Form) > 0 && len(request.Body) == 0 {
		httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for key, value := range request.Headers {
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		httpRequest.Header.Set(key, value)
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = providerTimeout
	}
	response, err := d.doCatalogRequestWithTimeout(httpRequest, timeout)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, duanjuMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > duanjuMaxBodyBytes {
		return nil, errors.New("站源返回内容过大")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &requestBackoff{status: response.StatusCode, host: duanjuHostOf(address), until: time.Now().Add(time.Minute)}
	}
	return payload, nil
}

func duanjuHostOf(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func (d *Downloader) duanjuJSON(ctx context.Context, request duanjuRequest) (map[string]any, error) {
	payload, err := d.duanjuDo(ctx, request)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, errors.New("站源返回的数据格式无法识别")
	}
	return decoded, nil
}

func duanjuFindString(node map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, found := node[key]; found {
			if text := nativeText(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func duanjuFindInt(node map[string]any, keys ...string) int {
	for _, key := range keys {
		if value, found := node[key]; found {
			switch typed := value.(type) {
			case json.Number:
				if number, err := typed.Int64(); err == nil {
					return int(number)
				}
			case float64:
				return int(typed)
			case string:
				if number, err := parseDuanjuNumber(typed); err == nil {
					return number
				}
			}
		}
	}
	return 0
}

func parseDuanjuNumber(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if number, err := strconv.Atoi(trimmed); err == nil {
		return number, nil
	}
	trimmed = strings.TrimSuffix(strings.TrimSuffix(trimmed, "集"), "全集")
	trimmed = strings.TrimSpace(trimmed)
	if number, err := strconv.Atoi(trimmed); err == nil {
		return number, nil
	}
	return 0, errors.New("不是数字")
}

func duanjuFindMap(node map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, found := node[key]; found {
			if mapped, ok := value.(map[string]any); ok {
				return mapped
			}
		}
	}
	return nil
}

func duanjuFindList(node map[string]any, keys ...string) []any {
	for _, key := range keys {
		if value, found := node[key]; found {
			if listed, ok := value.([]any); ok {
				return listed
			}
		}
	}
	return nil
}

func duanjuMapList(value any) []map[string]any {
	listed, ok := value.([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(listed))
	for _, entry := range listed {
		if mapped, ok := entry.(map[string]any); ok {
			rows = append(rows, mapped)
		}
	}
	return rows
}

func duanjuStringList(value any) []string {
	listed, ok := value.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(listed))
	for _, entry := range listed {
		switch typed := entry.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				values = append(values, text)
			}
		case map[string]any:
			if text := duanjuFindString(typed, "name", "title", "class_name"); text != "" {
				values = append(values, text)
			}
		}
	}
	return values
}

func duanjuCSV(values []string) string {
	return strings.Join(values, ", ")
}

func duanjuCover(base string, node map[string]any, keys ...string) string {
	for _, key := range keys {
		if address := providerCoverAddress(node[key], base); address != "" {
			return address
		}
	}
	return ""
}

func duanjuReleaseStatus(raw string, total, current int) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "over", "finished", "completed", "2", "已完结", "完结":
		return "finished"
	case "ongoing", "serializing", "1", "连载中", "更新中":
		return "ongoing"
	}
	if total > 0 && current >= total {
		return "finished"
	}
	if current > 0 {
		return "ongoing"
	}
	return ""
}

func duanjuEpisodeNumber(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if number := episodeIndex(raw, 0); number > 0 {
		return number
	}
	if number, err := strconv.Atoi(raw); err == nil && number > 0 {
		return number
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func duanjuChapter(source, sourceID string, number int, title, videoURL, pageURL, referer string) Chapter {
	if number <= 0 {
		number = 1
	}
	if strings.TrimSpace(title) == "" {
		title = "第" + strconv.Itoa(number) + "集"
	}
	return Chapter{
		ID:             providerChapterID(source, sourceID, strconv.Itoa(number)),
		Source:         source,
		Title:          truncate(title, 256),
		CurrentEpisode: rawEpisode(number),
		VideoURL:       strings.TrimSpace(videoURL),
		PageURL:        strings.TrimSpace(pageURL),
		Referer:        strings.TrimSpace(referer),
	}
}

func duanjuChapterKey(number int) string { return strconv.Itoa(number) }
