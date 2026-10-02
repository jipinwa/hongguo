package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nativeLANLimit = 768 << 10

var nativeLANID = regexp.MustCompile("^[a-f0-9]{32}$")
var nativeLANPin = regexp.MustCompile("^[a-f0-9]{64}$")

type nativeLANIdentity struct {
	ID          string `json:"id"`
	Certificate string `json:"certificate"`
	Key         string `json:"key"`
}

type nativeLANConfig struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Account  string   `json:"account"`
	User     string   `json:"user"`
	Sources  []string `json:"sources"`
	AutoSync bool     `json:"autoSync"`
}

type nativeLANInput struct {
	Generation string          `json:"generation"`
	Config     nativeLANConfig `json:"config"`
	DeviceID   string          `json:"deviceId"`
	Pin        string          `json:"pin"`
	Address    string          `json:"address"`
	Token      string          `json:"token"`
	Path       string          `json:"path"`
	RequestID  string          `json:"requestId"`
	EventID    string          `json:"eventId"`
	Payload    json.RawMessage `json:"payload"`
	Error      string          `json:"error"`
}

type nativeLANEvent struct {
	ID      string          `json:"id"`
	Path    string          `json:"path"`
	PeerID  string          `json:"peerId"`
	Pin     string          `json:"pin"`
	Host    string          `json:"host"`
	Payload json.RawMessage `json:"payload"`
}

type nativeLANReply struct {
	Payload json.RawMessage
	Error   string
}

type nativeLANRate struct {
	start time.Time
	count int
}

type nativeLANServer struct {
	mu          sync.Mutex
	identity    nativeLANIdentity
	cert        tls.Certificate
	pin         string
	generation  string
	config      nativeLANConfig
	server      *http.Server
	listener    net.Listener
	events      chan nativeLANEvent
	pending     map[string]chan nativeLANReply
	requests    map[string]context.CancelFunc
	nextRequest time.Time
	rates       map[string]nativeLANRate
	allowedID   string
	allowedPin  string
	token       string
	seen        time.Time
	done        chan struct{}
	closeOnce   sync.Once
	slots       chan struct{}
}

func nativeLANRandom(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("无法创建设备连接身份")
	}
	return hex.EncodeToString(value), nil
}

func nativeLANCertificate(directory string) (nativeLANIdentity, tls.Certificate, error) {
	var identity nativeLANIdentity
	file := filepath.Join(directory, "lan-identity.json")
	data, err := os.ReadFile(file)
	if err == nil {
		if len(data) > 32768 || json.Unmarshal(data, &identity) != nil || !nativeLANID.MatchString(identity.ID) {
			return identity, tls.Certificate{}, errors.New("设备互联身份损坏，原身份文件已保留")
		}
		cert, loadErr := tls.X509KeyPair([]byte(identity.Certificate), []byte(identity.Key))
		if loadErr != nil {
			return identity, cert, errors.New("无法读取设备互联证书")
		}
		return identity, cert, nil
	}
	if !os.IsNotExist(err) {
		return identity, tls.Certificate{}, err
	}
	id, err := nativeLANRandom(16)
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: id},
		NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true, DNSNames: []string{id + ".local"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	identity = nativeLANIdentity{
		ID:          id,
		Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		Key:         string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}
	data, err = json.Marshal(identity)
	if err == nil {
		err = writeNativeCacheFile(file, data)
	}
	if err != nil {
		return identity, tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair([]byte(identity.Certificate), []byte(identity.Key))
	return identity, cert, err
}

func nativeLANPrivate(host string) bool {
	host, _, _ = strings.Cut(strings.Trim(host, "[]"), "%")
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback())
}

func nativeLANAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 1 || number > 65535 || !nativeLANPrivate(host) {
		return "", errors.New("请输入局域网 IP 与端口，例如 192.168.1.8:53120")
	}
	return net.JoinHostPort(host, port), nil
}

func nativeLANFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

func nativeLANValidateConfig(config nativeLANConfig) error {
	if strings.TrimSpace(config.Name) == "" || len(config.Name) > 240 ||
		len(config.User) > 240 || !nativeLANID.MatchString(config.Account) ||
		len(config.Sources) > 32 ||
		(config.Kind != "phone" && config.Kind != "computer" && config.Kind != "tv") {
		return errors.New("设备互联配置无效")
	}
	seen := map[string]bool{}
	for _, source := range config.Sources {
		if !nativeSourceAvailable(source) || seen[source] {
			return errNativeBuildSource
		}
		seen[source] = true
	}
	return nil
}

func newNativeLANServer(directory string, config nativeLANConfig) (*nativeLANServer, error) {
	if err := nativeLANValidateConfig(config); err != nil {
		return nil, err
	}
	identity, cert, err := nativeLANCertificate(directory)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	if leaf.Subject.CommonName != identity.ID || time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) ||
		leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) != nil {
		return nil, errors.New("设备互联证书失效，原身份文件已保留")
	}
	generation, err := nativeLANRandom(16)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, errors.New("无法打开局域网接收端口")
	}
	link := &nativeLANServer{
		identity: identity, cert: cert, pin: nativeLANFingerprint(leaf), generation: generation, config: config,
		listener: listener, events: make(chan nativeLANEvent, 16), pending: map[string]chan nativeLANReply{},
		requests: map[string]context.CancelFunc{}, rates: map[string]nativeLANRate{}, done: make(chan struct{}),
		slots: make(chan struct{}, 16),
	}
	link.server = &http.Server{
		Handler:           http.HandlerFunc(link.serve),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 65 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert},
			ClientAuth: tls.RequireAnyClientCert,
		},
	}
	go func() { _ = link.server.Serve(tls.NewListener(listener, link.server.TLSConfig)) }()
	return link, nil
}

func (link *nativeLANServer) close() {
	link.closeOnce.Do(func() {
		close(link.done)
		_ = link.server.Close()
		link.mu.Lock()
		for _, cancel := range link.requests {
			cancel()
		}
		link.pending = map[string]chan nativeLANReply{}
		link.token, link.allowedID, link.allowedPin = "", "", ""
		link.mu.Unlock()
	})
}

func (link *nativeLANServer) info(private bool) map[string]any {
	link.mu.Lock()
	defer link.mu.Unlock()
	addresses := []string{}
	interfaces, _ := net.Interfaces()
	port := link.listener.Addr().(*net.TCPAddr).Port
	for _, network := range interfaces {
		if network.Flags&net.FlagUp == 0 || network.Flags&net.FlagLoopback != 0 {
			continue
		}
		rows, _ := network.Addrs()
		for _, row := range rows {
			host, _, err := net.ParseCIDR(row.String())
			if err != nil || !nativeLANPrivate(host.String()) || host.IsLoopback() {
				continue
			}
			text := host.String()
			if host.IsLinkLocalUnicast() && host.To4() == nil {
				text += "%" + network.Name
			}
			addresses = append(addresses, net.JoinHostPort(text, strconv.Itoa(port)))
		}
	}
	result := map[string]any{
		"deviceId": link.identity.ID, "name": link.config.Name, "kind": link.config.Kind,
		"pin": link.pin, "port": port, "protocol": 1,
	}
	if private {
		result["generation"], result["addresses"] = link.generation, addresses
		result["account"], result["user"] = link.config.Account, link.config.User
		result["sources"], result["autoSync"] = link.config.Sources, link.config.AutoSync
	}
	return result
}

func (engine *nativeEngine) nativeLAN(ctx context.Context, command string, raw json.RawMessage) (any, error) {
	var input nativeLANInput
	if len(raw) > nativeLANLimit || json.Unmarshal(raw, &input) != nil {
		return nil, errors.New("设备连接请求无效")
	}
	engine.lanMu.Lock()
	if command == "stop" {
		if engine.lan != nil && (input.Generation == "" || input.Generation == engine.lan.generation) {
			engine.lan.close()
			engine.lan = nil
		}
		engine.lanMu.Unlock()
		return true, nil
	}
	if command == "start" {
		if engine.lan != nil {
			engine.lan.close()
			engine.lan = nil
		}
		link, err := newNativeLANServer(engine.directory, input.Config)
		engine.lan = link
		engine.lanMu.Unlock()
		if err != nil {
			return nil, err
		}
		return link.info(true), nil
	}
	link := engine.lan
	engine.lanMu.Unlock()
	if link == nil || input.Generation != link.generation {
		return nil, errors.New("设备连接已失效，请重新连接")
	}
	switch command {
	case "configure":
		if err := nativeLANValidateConfig(input.Config); err != nil {
			return nil, err
		}
		link.mu.Lock()
		if input.Config.Account != link.config.Account {
			link.mu.Unlock()
			return nil, errors.New("当前用户已变更，请重新连接")
		}
		link.config = input.Config
		link.mu.Unlock()
		return true, nil
	case "allow":
		if !nativeLANID.MatchString(input.DeviceID) || !nativeLANPin.MatchString(input.Pin) || input.DeviceID == link.identity.ID {
			return nil, errors.New("目标设备身份无效")
		}
		link.mu.Lock()
		defer link.mu.Unlock()
		if link.allowedID != input.DeviceID || link.allowedPin != input.Pin || link.token == "" {
			token, err := nativeLANRandom(32)
			if err != nil {
				return nil, err
			}
			link.allowedID, link.allowedPin, link.token = input.DeviceID, input.Pin, token
		}
		link.seen = time.Now()
		return map[string]string{"token": link.token}, nil
	case "disconnect":
		link.mu.Lock()
		link.token, link.allowedID, link.allowedPin = "", "", ""
		for _, cancel := range link.requests {
			cancel()
		}
		link.mu.Unlock()
		return true, nil
	case "poll":
		timer := time.NewTimer(12 * time.Second)
		defer timer.Stop()
		select {
		case event := <-link.events:
			return map[string]any{"event": event}, nil
		case <-link.done:
			return nil, errors.New("设备接收已关闭")
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return map[string]any{}, nil
		}
	case "respond":
		link.mu.Lock()
		reply := link.pending[input.EventID]
		delete(link.pending, input.EventID)
		link.mu.Unlock()
		if reply == nil {
			return map[string]bool{"accepted": false}, nil
		}
		select {
		case reply <- nativeLANReply{input.Payload, input.Error}:
			return map[string]bool{"accepted": true}, nil
		default:
			return nil, errors.New("连接请求已结束")
		}
	case "cancel":
		link.mu.Lock()
		if cancel := link.requests[input.RequestID]; cancel != nil {
			cancel()
		}
		link.mu.Unlock()
		return true, nil
	case "request", "probe":
		return link.request(ctx, input, command == "probe")
	default:
		return nil, errors.New("设备互联操作无效")
	}
}
