package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type huangguoPreviewSession struct {
	mu      sync.Mutex
	token   string
	expires time.Time
	pending chan struct{}
}

func mediaRequestHeaders(request *http.Request, referer string) {
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("Sec-Fetch-Mode", "cors")
	request.Header.Set("Sec-Fetch-Dest", "empty")
	if origin, err := url.Parse(referer); err == nil && origin.Host != "" {
		request.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
	}
}

func (d *Downloader) isHuangguoVideoURL(address *url.URL) bool {
	configured, err := url.Parse(d.providerBaseURL(sourceHuangguoVideo))
	return address != nil && (strings.EqualFold(address.Hostname(), "huangguo.video") || err == nil && strings.EqualFold(address.Host, configured.Host))
}

func (d *Downloader) previewSession(origin string) *huangguoPreviewSession {
	d.previewMu.Lock()
	defer d.previewMu.Unlock()
	if d.previewSessions == nil {
		d.previewSessions = map[string]*huangguoPreviewSession{}
	}
	if d.previewSessions[origin] == nil {
		d.previewSessions[origin] = &huangguoPreviewSession{}
	}
	return d.previewSessions[origin]
}

func (d *Downloader) huangguoPreviewToken(ctx context.Context, origin, referer string) (string, error) {
	session := d.previewSession(origin)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		session.mu.Lock()
		if session.token != "" && time.Now().Add(15*time.Second).Before(session.expires) {
			token := session.token
			session.mu.Unlock()
			return token, nil
		}
		if pending := session.pending; pending != nil {
			session.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		pending := make(chan struct{})
		session.pending = pending
		session.mu.Unlock()
		token, expiry, err := d.fetchHuangguoPreviewToken(ctx, origin, referer)
		session.mu.Lock()
		if err == nil {
			session.token, session.expires = token, expiry
		}
		session.pending = nil
		close(pending)
		session.mu.Unlock()
		return token, err
	}
}

func (d *Downloader) fetchHuangguoPreviewToken(ctx context.Context, origin, referer string) (string, time.Time, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/preview-token", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	mediaRequestHeaders(request, referer)
	request.Header.Set("Accept", "application/json")
	response, err := d.doCatalogRequestWithTimeout(request, providerTimeout)
	if err != nil {
		return "", time.Time{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return "", time.Time{}, err
	}
	if response.StatusCode != http.StatusOK || catalogResponseBlockReason(response, body) != "" {
		return "", time.Time{}, d.catalogResponseError(request, response, body)
	}
	var result struct {
		OK        bool   `json:"ok"`
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &result) != nil || !result.OK || result.Token == "" || !validLegacyToken(result.Token) {
		return "", time.Time{}, errors.New("黄果视频未返回有效播放凭证，请稍后重试")
	}
	expiry := time.Now().Add(time.Duration(max(30, min(result.ExpiresIn, 1800))) * time.Second)
	if result.ExpiresAt > 0 {
		expiry = time.Unix(result.ExpiresAt, 0)
	}
	if !expiry.After(time.Now()) {
		return "", time.Time{}, errors.New("黄果视频播放凭证已过期，请检查设备时间")
	}
	return result.Token, expiry, nil
}

func (d *Downloader) doMediaRequest(request *http.Request) (*http.Response, error) {
	return d.doMediaRequestWithClient(request, d.client)
}

func (d *Downloader) doMediaRequestWithClient(request *http.Request, client *http.Client) (*http.Response, error) {
	mediaRequestHeaders(request, request.Header.Get("Referer"))
	if credentials, _ := request.Context().Value(providerMediaCredentialsKey{}).(*providerMediaCredentials); credentials != nil {
		if err := credentials.apply(request); err != nil {
			return nil, err
		}
		client = credentials.client(client)
	}
	keyRequest := d.isHuangguoVideoURL(request.URL) && strings.HasPrefix(request.URL.Path, "/api/hls_key/")
	origin := request.URL.Scheme + "://" + request.URL.Host
	for attempt := 0; attempt < 2; attempt++ {
		if keyRequest {
			token, err := d.huangguoPreviewToken(request.Context(), origin, request.Header.Get("Referer"))
			if err != nil {
				return nil, fmt.Errorf("获取播放凭证失败: %w", err)
			}
			request.Header.Set("X-Preview-Token", token)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		if keyRequest && attempt == 0 && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			if readErr == nil && catalogResponseBlockReason(response, body) == "" {
				session := d.previewSession(origin)
				session.mu.Lock()
				if session.token == request.Header.Get("X-Preview-Token") {
					session.token = ""
				}
				session.mu.Unlock()
				continue
			}
			response.Body = io.NopCloser(bytes.NewReader(body))
		}
		if d.limiter != nil {
			d.limiter.observe(request, response)
		}
		observeSourceResponse(request.Context(), response)
		return response, nil
	}
	return nil, errors.New("播放凭证刷新失败")
}

func (d *Downloader) fetchMediaPlaylist(ctx context.Context, address, referer string) (string, string, error) {
	if !isProviderHTTPMediaURL(address) {
		return "", "", errors.New("播放列表地址无效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Referer", referer)
	response, err := d.doMediaRequest(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return "", "", errors.New("播放列表过大或读取失败")
	}
	if response.StatusCode != http.StatusOK || catalogResponseBlockReason(response, body) != "" {
		return "", "", d.catalogResponseError(request, response, body)
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if !strings.HasPrefix(text, "#EXTM3U") {
		var envelope apiEnvelope
		if json.Unmarshal(body, &envelope) == nil && strings.Trim(string(envelope.Code), `"`) == "5005" {
			return "", "", &legacyAPIError{code: "5005", message: "播放会话已失效"}
		}
		return "", "", errors.New("站源未返回有效的播放列表，链接可能已失效")
	}
	finalURL := address
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	return text, finalURL, nil
}
