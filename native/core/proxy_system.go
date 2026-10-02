package core

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

type nativeSystemProxy struct {
	HTTP   string   `json:"http"`
	HTTPS  string   `json:"https"`
	Bypass []string `json:"bypass"`
	PAC    bool     `json:"pac"`
}

func (proxy nativeSystemProxy) resolve(request *http.Request) (*url.URL, error) {
	host := strings.ToLower(request.URL.Hostname())
	for _, exception := range proxy.Bypass {
		exception = strings.ToLower(strings.TrimSpace(exception))
		if exception == "<local>" && !strings.Contains(host, ".") && net.ParseIP(host) == nil {
			return nil, nil
		}
		if matched, _ := path.Match(exception, host); exception != "" && matched {
			return nil, nil
		}
	}
	address := proxy.HTTP
	if request.URL.Scheme == "https" {
		address = proxy.HTTPS
	}
	if address == "" {
		if proxy.PAC {
			return nil, errors.New("系统使用自动代理脚本，请在网络设置中配置手动代理或选择直连")
		}
		return http.ProxyFromEnvironment(request)
	}
	return url.Parse(address)
}

func (router *proxyRouter) systemSettings() nativeSystemProxy {
	router.mu.Lock()
	defer router.mu.Unlock()
	if !router.systemOverride && time.Since(router.systemRead) > 30*time.Second {
		router.system = platformSystemProxy()
		router.systemRead = time.Now()
	}
	return router.system
}

func (engine *nativeEngine) updateSystemProxy(proxy nativeSystemProxy) error {
	for _, address := range []string{proxy.HTTP, proxy.HTTPS} {
		if address == "" {
			continue
		}
		settings := defaultResourceSettings()
		settings.ProxyMode, settings.ProxyURL = "manual", address
		if err := settings.validate(); err != nil {
			return err
		}
	}
	if len(proxy.Bypass) > 1000 {
		return errors.New("系统代理排除列表过长")
	}
	router := engine.downloader.proxyRouter
	router.mu.Lock()
	if router.systemOverride && router.system.HTTP == proxy.HTTP && router.system.HTTPS == proxy.HTTPS &&
		router.system.PAC == proxy.PAC && slices.Equal(router.system.Bypass, proxy.Bypass) {
		router.mu.Unlock()
		return nil
	}
	router.system, router.systemOverride = proxy, true
	router.mu.Unlock()
	engine.downloader.client.CloseIdleConnections()
	return nil
}
