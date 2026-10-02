package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type requestGate struct {
	busy      bool
	lastStart time.Time
}

type requestBackoff struct {
	host   string
	until  time.Time
	status int
	reason string
}

func (backoff *requestBackoff) Error() string {
	reason := ""
	if backoff.reason != "" {
		reason = "：" + backoff.reason
	}
	seconds := max(1, int(time.Until(backoff.until).Seconds()+0.999))
	return fmt.Sprintf("%s HTTP %d%s，请在 %d 秒后重试", backoff.host, backoff.status, reason, seconds)
}

func catalogResponseBlockReason(response *http.Response, body []byte) string {
	if len(body) > 64*1024 {
		body = body[:64*1024]
	}
	page := strings.ToLower(string(body))
	reason := ""
	if strings.Contains(page, "cloudflare") && (strings.Contains(page, "sorry, you have been blocked") || strings.Contains(page, "you are unable to access")) {
		reason = "Cloudflare 拒绝了当前请求"
	} else if strings.EqualFold(response.Header.Get("Cf-Mitigated"), "challenge") || strings.Contains(page, "_cf_chl_opt") || strings.Contains(page, "<title>just a moment") && strings.Contains(page, "cloudflare") {
		reason = "站点要求浏览器验证，当前请求无法通过"
	}
	return reason
}

func (d *Downloader) catalogResponseError(request *http.Request, response *http.Response, body []byte) error {
	reason := catalogResponseBlockReason(response, body)
	if d.limiter != nil && reason != "" && response.StatusCode >= 200 && response.StatusCode < 300 {
		d.limiter.observeBlock(request, response)
	}

	host := strings.ToLower(request.URL.Hostname())
	if d.limiter != nil && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests || reason != "") {
		d.limiter.mu.Lock()
		backoffs := d.limiter.backoffs
		if background, _ := request.Context().Value(backgroundCatalogKey{}).(bool); background && response.StatusCode != http.StatusTooManyRequests {
			backoffs = d.limiter.backgroundBackoffs
		}
		if previous := backoffs[host]; previous != nil && previous.status == response.StatusCode {
			updated := *previous
			if reason != "" {
				updated.reason = reason
			}
			backoffs[host] = &updated
			d.limiter.mu.Unlock()
			return &updated
		}
		d.limiter.mu.Unlock()
	}
	if reason != "" {
		return fmt.Errorf("%s HTTP %d：%s", host, response.StatusCode, reason)
	}
	return fmt.Errorf("%s HTTP %d", host, response.StatusCode)
}

type requestLimiter struct {
	concurrency        int
	active             int
	foregroundWaiting  int
	lastForeground     time.Time
	interval           time.Duration
	mu                 sync.Mutex
	gates              map[string]*requestGate
	backoffs           map[string]*requestBackoff
	backgroundBackoffs map[string]*requestBackoff
	changed            chan struct{}
}

type backgroundCatalogKey struct{}

func newRequestLimiter(concurrency int, interval time.Duration) *requestLimiter {
	if concurrency < 1 {
		concurrency = 1
	}
	return &requestLimiter{concurrency: concurrency, interval: interval, gates: map[string]*requestGate{}, backoffs: map[string]*requestBackoff{}, backgroundBackoffs: map[string]*requestBackoff{}, changed: make(chan struct{})}
}

func (limiter *requestLimiter) notifyLocked() {
	close(limiter.changed)
	limiter.changed = make(chan struct{})
}

func (limiter *requestLimiter) acquire(ctx context.Context, request *http.Request) (func(), error) {
	background, _ := ctx.Value(backgroundCatalogKey{}).(bool)
	host := strings.ToLower(request.URL.Hostname())
	source := providerSourceForURL(request.URL.String())
	if source == "" {
		source = host
		if strings.HasSuffix(host, ".cloudfront.net") {
			source = "cloudfront"
		}
	}
	limiter.mu.Lock()
	if !background {
		limiter.foregroundWaiting++
		defer func() {
			limiter.mu.Lock()
			limiter.foregroundWaiting--
			limiter.notifyLocked()
			limiter.mu.Unlock()
		}()
	}
	gate := limiter.gates[source]
	if gate == nil {
		gate = &requestGate{}
		limiter.gates[source] = gate
	}
	limiter.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limiter.mu.Lock()
		if backoff := limiter.backoffs[host]; backoff != nil && time.Now().Before(backoff.until) {
			limiter.mu.Unlock()
			return nil, backoff
		}
		if backoff := limiter.backgroundBackoffs[host]; background && backoff != nil && time.Now().Before(backoff.until) {
			limiter.mu.Unlock()
			return nil, backoff
		}
		delay := time.Until(gate.lastStart.Add(limiter.interval))
		if background {

			if idleDelay := time.Until(limiter.lastForeground.Add(limiter.interval)); idleDelay > delay {
				delay = idleDelay
			}
		}
		lowPriorityReady := !background || limiter.foregroundWaiting == 0 && limiter.active == 0
		if lowPriorityReady && !gate.busy && limiter.active < limiter.concurrency && delay <= 0 {
			gate.busy = true
			gate.lastStart = time.Now()
			limiter.active++
			limiter.mu.Unlock()
			break
		}
		changed := limiter.changed
		limiter.mu.Unlock()
		var timer *time.Timer
		var deadline <-chan time.Time
		if delay > 0 {
			timer = time.NewTimer(delay)
			deadline = timer.C
		}
		select {
		case <-changed:
		case <-deadline:
		case <-ctx.Done():
		}
		if timer != nil {
			timer.Stop()
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			limiter.active--
			gate.busy = false
			if !background {
				limiter.lastForeground = time.Now()
			}
			limiter.notifyLocked()
			limiter.mu.Unlock()
		})
	}, nil
}

func (limiter *requestLimiter) observe(request *http.Request, response *http.Response) {
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusTooManyRequests && !strings.EqualFold(response.Header.Get("Cf-Mitigated"), "challenge") {
		return
	}
	limiter.observeBlock(request, response)
}

func (limiter *requestLimiter) observeBlock(request *http.Request, response *http.Response) {
	delay := time.Minute
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if until, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && time.Until(until) > 0 {
		delay = time.Until(until)
	}
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	host := strings.ToLower(request.URL.Hostname())
	limiter.mu.Lock()
	backoffs := limiter.backoffs
	if background, _ := request.Context().Value(backgroundCatalogKey{}).(bool); background && response.StatusCode != http.StatusTooManyRequests {

		backoffs = limiter.backgroundBackoffs
	}
	backoffs[host] = &requestBackoff{host: host, until: time.Now().Add(delay), status: response.StatusCode}
	limiter.notifyLocked()
	limiter.mu.Unlock()
}

type limitedResponseBody struct {
	io.ReadCloser
	release func()
}

func (body *limitedResponseBody) Close() error {
	err := body.ReadCloser.Close()
	body.release()
	return err
}

func (d *Downloader) doCatalogRequestWithTimeout(request *http.Request, timeout time.Duration) (*http.Response, error) {
	return d.doPreparedCatalogRequest(request, timeout, nil)
}

func (d *Downloader) doPreparedCatalogRequest(request *http.Request, timeout time.Duration, prepare func(*http.Request)) (*http.Response, error) {
	if background, _ := request.Context().Value(backgroundCatalogKey{}).(bool); background && (timeout <= 0 || timeout > 8*time.Second) {
		timeout = 8 * time.Second
	}
	release, err := d.limiter.acquire(request.Context(), request)
	if err != nil {
		return nil, err
	}
	cancel := func() {}
	if timeout > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(request.Context(), timeout)
		request = request.WithContext(ctx)
	}
	if prepare != nil {
		prepare(request)
	}
	response, err := d.client.Do(request)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	d.limiter.observe(request, response)
	observeSourceResponse(request.Context(), response)
	response.Body = &limitedResponseBody{ReadCloser: response.Body, release: func() { cancel(); release() }}
	return response, nil
}
