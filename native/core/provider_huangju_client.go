package core

import (
	"bytes"
	"context"
	"crypto/rand"
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

const (
	huangjuBaseURL    = "https://huangju.net"
	huangjuAPIBaseURL = "https://api.huangju.net"
	huangjuUserAgent  = "Mozilla/5.0 (Linux; Android 11; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.91 Mobile Safari/537.36"
)

var errHuangjuGuestExpired = errors.New("剧果访客授权已失效，请重试")

type huangjuGuestCall struct {
	done  chan struct{}
	token string
	err   error
}

type huangjuAPIClient struct {
	downloader *Downloader
	base       string
	site       string
	mu         sync.Mutex
	deviceID   string
	token      string
	pending    *huangjuGuestCall
}

type huangjuAPIResponse struct {
	value   any
	headers http.Header
}

func (d *Downloader) huangjuClient() *huangjuAPIClient {
	d.huangjuOnce.Do(func() {
		d.huangju = &huangjuAPIClient{
			downloader: d,
			base:       strings.TrimRight(firstNonEmpty(d.cfg.HuangjuAPIURL, huangjuAPIBaseURL), "/"),
			site:       d.providerBaseURL(sourceHuangju),
		}
	})
	return d.huangju
}

func (client *huangjuAPIClient) guestToken(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	client.mu.Lock()
	if client.token != "" {
		token := client.token
		client.mu.Unlock()
		return token, nil
	}
	if pending := client.pending; pending != nil {
		client.mu.Unlock()
		select {
		case <-pending.done:
			if (errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded)) && ctx.Err() == nil {
				return client.guestToken(ctx)
			}
			return pending.token, pending.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if client.deviceID == "" {
		var identifier [16]byte
		if _, err := rand.Read(identifier[:]); err != nil {
			client.mu.Unlock()
			return "", errors.New("无法初始化剧果访客会话")
		}
		identifier[6] = identifier[6]&0x0f | 0x40
		identifier[8] = identifier[8]&0x3f | 0x80
		client.deviceID = fmt.Sprintf("%x-%x-%x-%x-%x", identifier[:4], identifier[4:6], identifier[6:8], identifier[8:10], identifier[10:])
	}
	deviceID := client.deviceID
	pending := &huangjuGuestCall{done: make(chan struct{})}
	client.pending = pending
	client.mu.Unlock()

	body, _ := json.Marshal(map[string]string{"deviceId": deviceID})
	response, err := client.request(ctx, http.MethodPost, "/auth/guest", nil, body, "", false, 64<<10)
	token := ""
	if err == nil {
		row, _ := response.value.(map[string]any)
		token, _ = row["token"].(string)
		if token == "" {
			data, _ := row["data"].(map[string]any)
			token, _ = data["token"].(string)
		}
		if token == "" || len(token) > 8192 || strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
			token = ""
			err = errors.New("剧果未返回有效的访客授权")
		}
	}
	client.mu.Lock()
	if err == nil {
		client.token = token
	}
	pending.token, pending.err = token, err
	client.pending = nil
	close(pending.done)
	client.mu.Unlock()
	return token, err
}

func (client *huangjuAPIClient) get(ctx context.Context, route string, query url.Values, limit int64) (huangjuAPIResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := client.guestToken(ctx)
		if err != nil {
			return huangjuAPIResponse{}, err
		}
		response, err := client.request(ctx, http.MethodGet, route, query, nil, token, attempt == 0, limit)
		if attempt == 0 && errors.Is(err, errHuangjuGuestExpired) {
			client.mu.Lock()
			if client.token == token {
				client.token = ""
			}
			client.mu.Unlock()
			continue
		}
		return response, err
	}
	return huangjuAPIResponse{}, errHuangjuGuestExpired
}

func (client *huangjuAPIClient) request(ctx context.Context, method, route string, query url.Values, body []byte, token string, retryAuth bool, limit int64) (huangjuAPIResponse, error) {
	base, err := url.Parse(client.base)
	if err != nil || !isProviderHTTPMediaURL(client.base) || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return huangjuAPIResponse{}, errors.New("剧果接口地址无效")
	}
	timeout := 15 * time.Second
	if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	address := client.base + route
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return huangjuAPIResponse{}, errors.New("无法创建剧果请求")
	}
	request.Header.Set("User-Agent", huangjuUserAgent)
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("Referer", client.site+"/")
	request.Header.Set("Origin", client.site)
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	d := client.downloader
	if d.limiter != nil {
		release, err := d.limiter.acquire(ctx, request)
		if err != nil {
			return huangjuAPIResponse{}, err
		}
		defer release()
	}
	transport := *d.client
	transport.Jar = nil
	previousRedirect := transport.CheckRedirect
	transport.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || providerMediaOrigin(next.URL) != providerMediaOrigin(base) {
			return errors.New("剧果接口重定向地址异常")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := transport.Do(request)
	if err != nil {
		return huangjuAPIResponse{}, fmt.Errorf("剧果连接失败：%w", publicError(err))
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	blocked := catalogResponseBlockReason(response, data) != ""
	authFailed := response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden
	if retryAuth && token != "" && authFailed && readErr == nil && int64(len(data)) <= limit && !blocked && json.Valid(data) {
		return huangjuAPIResponse{}, errHuangjuGuestExpired
	}
	if d.limiter != nil {
		d.limiter.observe(request, response)
	}
	observeSourceResponse(ctx, response)
	if response.StatusCode < 200 || response.StatusCode >= 300 || blocked {
		return huangjuAPIResponse{}, d.catalogResponseError(request, response, data)
	}
	if readErr != nil || int64(len(data)) > limit {
		return huangjuAPIResponse{}, errors.New("剧果返回的数据过大或读取失败")
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return huangjuAPIResponse{}, errors.New("剧果返回的数据格式无效")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return huangjuAPIResponse{}, errors.New("剧果返回的数据格式无效")
	}
	return huangjuAPIResponse{value: decoded, headers: response.Header.Clone()}, nil
}

func huangjuPayload(value any) any {
	if row, ok := value.(map[string]any); ok && row["data"] != nil && row["id"] == nil && row["items"] == nil && row["url"] == nil {
		return row["data"]
	}
	return value
}
