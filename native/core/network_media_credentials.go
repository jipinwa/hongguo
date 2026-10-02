package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type providerMediaCredentials struct {
	cookie    string
	origin    string
	referer   string
	userAgent string
	expires   time.Time
}

type providerMediaCredentialsKey struct{}

func providerMediaContext(ctx context.Context, credentials *providerMediaCredentials) context.Context {
	return context.WithValue(ctx, providerMediaCredentialsKey{}, credentials)
}

func providerMediaOrigin(address *url.URL) string {
	if address == nil {
		return ""
	}
	scheme := strings.ToLower(address.Scheme)
	host := strings.ToLower(address.Host)
	if scheme == "https" && address.Port() == "443" || scheme == "http" && address.Port() == "80" {
		host = strings.TrimSuffix(host, ":"+address.Port())
	}
	return scheme + "://" + host
}

func (credentials *providerMediaCredentials) apply(request *http.Request) error {
	request.Header.Del("Cookie")
	if credentials.userAgent != "" {
		request.Header.Set("User-Agent", credentials.userAgent)
	}
	if credentials.referer != "" {
		request.Header.Set("Referer", credentials.referer)
		if address, err := url.Parse(credentials.referer); err == nil {
			request.Header.Set("Origin", providerMediaOrigin(address))
		}
	}
	if providerMediaOrigin(request.URL) == credentials.origin {
		if !credentials.expires.IsZero() && !time.Now().Before(credentials.expires) {
			return errors.New("播放凭证已过期，请重新解析播放")
		}
		request.Header.Set("Cookie", credentials.cookie)
	}
	return nil
}

func (credentials *providerMediaCredentials) client(client *http.Client) *http.Client {
	scoped := *client
	scoped.Jar = nil
	previousRedirect := client.CheckRedirect
	scoped.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("媒体重定向过多")
		}
		if previousRedirect != nil {
			if err := previousRedirect(request, via); err != nil {
				return err
			}
		}
		return credentials.apply(request)
	}
	return &scoped
}
