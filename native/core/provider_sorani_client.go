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

const (
	soraniSiteBaseURL = "https://www.sorani.net"
	soraniAPIBaseURL  = "https://api.sorani.cc/sorani-cms/api"
	soraniUserAgent   = "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
)

type soraniAPIClient struct {
	downloader *Downloader
	base       string
	site       string
	once       sync.Once
}

func (d *Downloader) soraniClient() *soraniAPIClient {
	d.soraniOnce.Do(func() {
		d.sorani = &soraniAPIClient{
			downloader: d,
			base:       strings.TrimRight(firstNonEmpty(d.cfg.SoraniAPIURL, soraniAPIBaseURL), "/"),
			site:       d.providerBaseURL(sourceSorani),
		}
	})
	return d.sorani
}

func (client *soraniAPIClient) get(ctx context.Context, route string, query url.Values, limit int64) (any, error) {
	if limit <= 0 {
		limit = 4 << 20
	}
	base, err := url.Parse(client.base)
	if err != nil || !isProviderHTTPMediaURL(client.base) || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("青空次元接口地址无效")
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("无法创建青空次元请求")
	}
	request.Header.Set("User-Agent", soraniUserAgent)
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	request.Header.Set("Referer", client.site+"/")
	request.Header.Set("Origin", client.site)

	d := client.downloader
	if d.limiter != nil {
		release, err := d.limiter.acquire(ctx, request)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	// 与黄剧客户端一致：接口请求不携带共享 Cookie Jar，且只允许同源重定向。
	transport := *d.client
	transport.Jar = nil
	previousRedirect := transport.CheckRedirect
	transport.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || providerMediaOrigin(next.URL) != providerMediaOrigin(base) {
			return errors.New("青空次元接口重定向地址异常")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	response, err := transport.Do(request)
	if err != nil {
		return nil, fmt.Errorf("青空次元连接失败：%w", publicError(err))
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if d.limiter != nil {
		d.limiter.observe(request, response)
	}
	observeSourceResponse(ctx, response)
	blocked := catalogResponseBlockReason(response, data) != ""
	if response.StatusCode < 200 || response.StatusCode >= 300 || blocked {
		return nil, d.catalogResponseError(request, response, data)
	}
	if readErr != nil || int64(len(data)) > limit {
		return nil, errors.New("青空次元返回的数据过大或读取失败")
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("青空次元返回的数据格式无效")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("青空次元返回的数据格式无效")
	}
	// 上游统一用 code=200 表示成功；非 200 一律视为业务错误。
	if row, ok := decoded.(map[string]any); ok {
		if code := nativeText(row["code"]); code != "" && code != "200" {
			message := mapString(row, "message", "msg")
			if message == "" {
				message = "上游返回错误码 " + code
			}
			return nil, errors.New("青空次元接口错误：" + truncate(message, 120))
		}
	}
	return decoded, nil
}
