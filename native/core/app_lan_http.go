package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (link *nativeLANServer) request(parent context.Context, input nativeLANInput, probe bool) (any, error) {
	address, err := nativeLANAddress(input.Address)
	if err != nil {
		return nil, err
	}
	if !probe && (!nativeLANPin.MatchString(input.Pin) || !nativeLANID.MatchString(input.DeviceID)) {
		return nil, errors.New("目标设备证书无效")
	}
	if probe {
		input.Path = "hello"
	}
	if !nativeLANPath(input.Path) || len(input.Payload) > nativeLANLimit-16384 || !nativeLANID.MatchString(input.RequestID) {
		return nil, errors.New("设备请求无效")
	}
	ctx, cancel := context.WithTimeout(parent, 55*time.Second)
	defer cancel()
	link.mu.Lock()
	select {
	case <-link.done:
		link.mu.Unlock()
		return nil, errors.New("设备接收已关闭")
	default:
	}
	if len(link.requests) >= 8 || link.requests[input.RequestID] != nil {
		link.mu.Unlock()
		return nil, errors.New("设备连接正忙，请稍后重试")
	}
	link.requests[input.RequestID] = cancel
	now := time.Now()
	start := link.nextRequest
	if start.Before(now) {
		start = now
	}
	link.nextRequest = start.Add(50 * time.Millisecond)
	link.mu.Unlock()
	defer func() {
		link.mu.Lock()
		delete(link.requests, input.RequestID)
		link.mu.Unlock()
	}()
	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-link.done:
			return nil, errors.New("设备接收已关闭")
		}
	}
	var observedPin, observedID string
	transport := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 7 * time.Second}).DialContext,
		TLSHandshakeTimeout: 7 * time.Second, ResponseHeaderTimeout: 55 * time.Second,
		DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{link.cert},
			InsecureSkipVerify: true,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) != 1 {
					return errors.New("设备证书无效")
				}
				cert := state.PeerCertificates[0]
				observedPin, observedID = nativeLANFingerprint(cert), cert.Subject.CommonName
				if !nativeLANID.MatchString(observedID) || time.Now().Before(cert.NotBefore) ||
					time.Now().After(cert.NotAfter) ||
					cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil {
					return errors.New("设备证书无效或已过期")
				}
				if input.Pin != "" && subtle.ConstantTimeCompare([]byte(input.Pin), []byte(observedPin)) != 1 {
					return errors.New("设备证书已改变，请在设备列表中核对后重新配对")
				}
				if input.DeviceID != "" && input.DeviceID != observedID {
					return errors.New("设备身份与已记住的目标不一致")
				}
				return nil
			},
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("设备连接不接受重定向")
	}}
	body := input.Payload
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	target := url.URL{Scheme: "https", Host: address, Path: "/zgj/v1/" + input.Path}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Zgj-Device", link.identity.ID)
	if !probe && input.Token != "" {
		request.Header.Set("Authorization", "Bearer "+input.Token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("局域网连接失败：%w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeLANLimit+1))
	if err != nil || len(data) > nativeLANLimit {
		return nil, errors.New("对方返回的数据过大或传输中断")
	}
	var envelope struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return nil, errors.New("对方设备的协议版本不兼容")
	}
	if response.StatusCode != http.StatusOK || !envelope.OK {
		message := envelope.Error
		if message == "" || len(message) > 1000 {
			message = "对方未接受连接，请重新选择设备"
		}
		return nil, errors.New(message)
	}
	if probe {
		var info map[string]any
		if json.Unmarshal(envelope.Data, &info) != nil || info["deviceId"] != observedID || info["pin"] != observedPin {
			return nil, errors.New("设备身份响应不一致")
		}
		info["address"] = address
		return info, nil
	}
	return envelope.Data, nil
}

func nativeLANPath(path string) bool {
	switch path {
	case "hello", "pair", "status", "sync/summary", "sync/records", "sync/begin",
		"sync/chunk", "sync/commit", "sync/cancel", "sync/receipt",
		"play/prepare", "play/start", "play/status", "play/cancel":
		return true
	}
	return false
}

func nativeLANWrite(w http.ResponseWriter, status int, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	body := map[string]any{"ok": err == nil}
	if err != nil {
		body["error"] = err.Error()
	} else {
		body["data"] = data
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (link *nativeLANServer) serve(w http.ResponseWriter, request *http.Request) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || !nativeLANPrivate(host) || request.Method != http.MethodPost ||
		request.URL.RawQuery != "" || request.Header.Get("Origin") != "" ||
		request.TLS == nil || len(request.TLS.PeerCertificates) != 1 {
		nativeLANWrite(w, http.StatusForbidden, nil, errors.New("不接受此设备请求"))
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/zgj/v1/")
	if !strings.HasPrefix(request.URL.Path, "/zgj/v1/") || !nativeLANPath(path) {
		nativeLANWrite(w, http.StatusNotFound, nil, errors.New("设备操作不存在"))
		return
	}
	cert := request.TLS.PeerCertificates[0]
	peerID, pin := cert.Subject.CommonName, nativeLANFingerprint(cert)
	if !nativeLANID.MatchString(peerID) || peerID == link.identity.ID ||
		request.Header.Get("X-Zgj-Device") != peerID ||
		time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) ||
		cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature) != nil {
		nativeLANWrite(w, http.StatusForbidden, nil, errors.New("设备身份校验失败"))
		return
	}
	select {
	case link.slots <- struct{}{}:
		defer func() { <-link.slots }()
	default:
		nativeLANWrite(w, http.StatusTooManyRequests, nil, errors.New("设备正忙，请稍后重试"))
		return
	}
	link.mu.Lock()
	now := time.Now()
	for address, rate := range link.rates {
		if now.Sub(rate.start) > time.Minute {
			delete(link.rates, address)
		}
	}
	rate := link.rates[host]
	if now.Sub(rate.start) > time.Second {
		rate = nativeLANRate{start: now}
	}
	rate.count++
	if len(link.rates) < 128 || link.rates[host].count > 0 {
		link.rates[host] = rate
	} else {
		rate.count = 31
	}
	allowed := path == "hello" || path == "pair" ||
		link.allowedID == peerID && link.allowedPin == pin && link.token != "" &&
			now.Sub(link.seen) < 2*time.Minute &&
			subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+link.token)) == 1
	if allowed && path != "hello" && path != "pair" {
		link.seen = now
	}
	link.mu.Unlock()
	if rate.count > 30 || len(request.Header.Get("Authorization")) > 128 {
		nativeLANWrite(w, http.StatusTooManyRequests, nil, errors.New("设备请求过于频繁"))
		return
	}
	if !allowed {
		nativeLANWrite(w, http.StatusUnauthorized, nil, errors.New("连接或用户会话已失效，请重新连接"))
		return
	}
	if path == "hello" {
		nativeLANWrite(w, http.StatusOK, link.info(false), nil)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, request.Body, nativeLANLimit-16384))
	if err != nil || !json.Valid(data) {
		nativeLANWrite(w, http.StatusBadRequest, nil, errors.New("设备记录格式或大小无效"))
		return
	}
	id, err := nativeLANRandom(16)
	if err != nil {
		nativeLANWrite(w, http.StatusInternalServerError, nil, err)
		return
	}
	reply := make(chan nativeLANReply, 1)
	link.mu.Lock()
	link.pending[id] = reply
	link.mu.Unlock()
	defer func() {
		link.mu.Lock()
		delete(link.pending, id)
		link.mu.Unlock()
	}()
	timer := time.NewTimer(50 * time.Second)
	defer timer.Stop()
	select {
	case link.events <- nativeLANEvent{id, path, peerID, pin, host, data}:
	case <-link.done:
		return
	case <-request.Context().Done():
		return
	case <-timer.C:
		nativeLANWrite(w, http.StatusServiceUnavailable, nil, errors.New("对方应用未能及时处理请求"))
		return
	}
	select {
	case result := <-reply:
		if result.Error != "" {
			if len(result.Error) > 1000 {
				result.Error = "对方未能完成本次操作"
			}
			nativeLANWrite(w, http.StatusConflict, nil, errors.New(result.Error))
		} else {
			nativeLANWrite(w, http.StatusOK, result.Payload, nil)
		}
	case <-link.done:
		return
	case <-request.Context().Done():
		return
	case <-timer.C:
		nativeLANWrite(w, http.StatusGatewayTimeout, nil, errors.New("对方应用响应超时，请保持前台并重试"))
	}
}
