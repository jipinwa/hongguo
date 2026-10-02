package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type legacyProtocol struct {
	InterfaceKey string `json:"interfaceKey"`
	ParamKey     string `json:"paramKey"`
	ParamIV      string `json:"paramIV"`
}

type legacySessionState struct {
	Version   int            `json:"version"`
	Scope     string         `json:"scope"`
	DeviceID  string         `json:"deviceId"`
	Token     string         `json:"token"`
	Protocol  legacyProtocol `json:"protocol"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type legacyAccess struct {
	legacyProtocol
	Token     string
	DeviceID  string
	Anonymous bool
}

type legacyAPIClient struct {
	mu      sync.Mutex
	loaded  bool
	state   legacySessionState
	access  *legacyAccess
	pending chan struct{}
	err     error
	retryAt time.Time
}

func (d *Downloader) legacyClient() *legacyAPIClient {
	d.legacyOnce.Do(func() { d.legacy = &legacyAPIClient{} })
	return d.legacy
}

func (d *Downloader) legacyCredentials(ctx context.Context) (legacyAccess, error) {
	client := d.legacyClient()
	for {
		if err := ctx.Err(); err != nil {
			return legacyAccess{}, err
		}
		client.mu.Lock()
		if client.access != nil {
			access := *client.access
			client.mu.Unlock()
			return access, nil
		}
		if pending := client.pending; pending != nil {
			client.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return legacyAccess{}, ctx.Err()
			}
		}
		if client.err != nil && time.Now().Before(client.retryAt) {
			err := client.err
			client.mu.Unlock()
			return legacyAccess{}, err
		}
		if !client.loaded {
			client.state = d.loadLegacySession()
			client.loaded = true
		}
		state := client.state
		pending := make(chan struct{})
		client.pending = pending
		client.mu.Unlock()

		access, updated, err := d.bootstrapLegacySession(ctx, state)
		client.mu.Lock()
		client.state = updated
		client.err = err
		client.retryAt = time.Time{}
		if err == nil {
			client.access = &access
		} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			client.retryAt = time.Now().Add(time.Minute)
		}
		client.pending = nil
		close(pending)
		client.mu.Unlock()
		return access, err
	}
}

func (d *Downloader) legacySessionScope() string {
	return strings.TrimRight(firstNonEmpty(d.cfg.APIBase, defaultAPIBase), "/")
}

func (d *Downloader) loadLegacySession() legacySessionState {
	empty := legacySessionState{Version: 1, Scope: d.legacySessionScope()}
	file, err := os.Open(filepath.Join(d.cfg.dataDir, "huangguo-session.json"))
	if err != nil {
		return empty
	}
	defer file.Close()
	var state legacySessionState
	if json.NewDecoder(io.LimitReader(file, 16*1024)).Decode(&state) != nil || state.Version != 1 || state.Scope != empty.Scope ||
		!regexp.MustCompile(`^[A-Fa-f0-9]{16}[0-9]{13}$`).MatchString(state.DeviceID) || !validLegacyToken(state.Token) {
		return empty
	}
	return state
}

func validLegacyToken(token string) bool {
	return len(token) <= 4096 && !strings.ContainsAny(token, "\r\n\t ")
}

func (d *Downloader) saveLegacySession(state legacySessionState) error {
	directory := d.cfg.dataDir
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".huangguo-session-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(state); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, "huangguo-session.json"))
}

func (d *Downloader) bootstrapLegacySession(ctx context.Context, state legacySessionState) (legacyAccess, legacySessionState, error) {
	access := legacyAccess{Token: d.cfg.Token, Anonymous: d.cfg.Token == ""}
	protocol := state.Protocol
	needsProtocol := d.cfg.InterfaceKey == "" || d.cfg.ParamKey == "" || d.cfg.ParamIV == ""
	if needsProtocol && (!validLegacyProtocol(protocol) || time.Since(state.UpdatedAt) > 24*time.Hour) {
		script, err := d.fetchLegacyFrontendScript(ctx)
		if err != nil {
			return access, state, fmt.Errorf("获取黄果官网协议失败: %w", publicError(err))
		}
		protocol, err = parseLegacyProtocol(script)
		if err != nil {
			return access, state, err
		}
		state.Protocol, state.UpdatedAt = protocol, time.Now()
	}
	access.legacyProtocol = legacyProtocol{
		InterfaceKey: firstNonEmpty(d.cfg.InterfaceKey, protocol.InterfaceKey),
		ParamKey:     firstNonEmpty(d.cfg.ParamKey, protocol.ParamKey),
		ParamIV:      firstNonEmpty(d.cfg.ParamIV, protocol.ParamIV),
	}
	if !validLegacyProtocol(access.legacyProtocol) || !validLegacyToken(access.Token) {
		return access, state, errors.New("黄果 API 协议配置无效，请更新应用后重试")
	}
	if access.Anonymous {
		if state.DeviceID == "" {
			prefix := strings.ToUpper(randomHex(8))
			if prefix == "" {
				return access, state, errors.New("无法生成黄果访客标识")
			}
			state.DeviceID = prefix + strconv.FormatInt(time.Now().UnixMilli(), 10)
		}
		access.DeviceID, access.Token = state.DeviceID, state.Token
		if access.Token == "" {
			payload, err := d.legacyRequest(ctx, http.MethodPost, "/api/app/mine/login/h5", map[string]any{
				"devID": access.DeviceID, "sysType": "ios", "isAppStore": false,
			}, access)
			if err != nil {
				return access, state, fmt.Errorf("黄果访客登录失败: %w", err)
			}
			var login struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(payload, &login) != nil || login.Token == "" || !validLegacyToken(login.Token) {
				return access, state, errors.New("黄果访客登录未返回有效令牌")
			}
			access.Token, state.Token = login.Token, login.Token
		}
	}
	if access.Anonymous {
		if err := d.saveLegacySession(state); err != nil {
			fmt.Println("黄果访客会话未能保存，本次会话仍可使用")
		}
	}
	return access, state, nil
}

func validLegacyProtocol(protocol legacyProtocol) bool {
	return len(protocol.InterfaceKey) > 0 && len(protocol.InterfaceKey) <= 512 && len(protocol.ParamKey) == 16 && len(protocol.ParamIV) == 16
}

func parseLegacyProtocol(script string) (legacyProtocol, error) {
	value := func(name string) string {
		pattern := `["']` + regexp.QuoteMeta(name) + `["']\s*[:,]\s*["']([^"'\\\r\n]+)["']`
		match := regexp.MustCompile(pattern).FindStringSubmatch(script)
		if len(match) == 2 {
			return match[1]
		}
		return ""
	}
	protocol := legacyProtocol{InterfaceKey: value("interfaceKey"), ParamKey: value("parameterKey"), ParamIV: value("parameterIv")}
	if !validLegacyProtocol(protocol) {
		return legacyProtocol{}, errors.New("黄果官网协议已变化，无法识别当前接口参数")
	}
	return protocol, nil
}

func (d *Downloader) invalidateLegacyToken(access legacyAccess) {
	client := d.legacyClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	if access.Anonymous && client.access != nil && client.access.Token == access.Token {
		client.state.Token = ""
		client.access = nil
		client.err = nil
	}
}

func (access legacyAccess) userAgent() string {
	if access.DeviceID == "" {
		return xUserAgent
	}
	return strings.Replace(xUserAgent, "DevID=00000000000000000000000000000", "DevID="+access.DeviceID, 1)
}
