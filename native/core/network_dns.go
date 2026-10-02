package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const dnsAlternateSubnet = "8.8.8.0/24"

type dnsCacheEntry struct {
	addresses []string
	expires   time.Time
}

type dnsResolver struct {
	client    *http.Client
	mu        sync.Mutex
	cache     map[string]dnsCacheEntry
	inFlight  map[string]chan struct{}
	endpoints []string
}

func newDNSResolver(transport *http.Transport) *dnsResolver {
	lookupTransport := transport.Clone()
	lookupTransport.TLSClientConfig = nil
	return &dnsResolver{
		client: &http.Client{Transport: lookupTransport, Timeout: 2 * time.Second},
		cache:  map[string]dnsCacheEntry{}, inFlight: map[string]chan struct{}{},
		endpoints: []string{
			"https://doh.pub/resolve",
			"https://dns.alidns.com/resolve",
		},
	}
}

func (resolver *dnsResolver) lookup(ctx context.Context, host string) ([]string, error) {
	entry, err := resolver.lookupEntry(ctx, host, "")
	return entry.addresses, err
}

func (resolver *dnsResolver) lookupEntry(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	cacheKey := host + "|" + subnet
	for {
		resolver.mu.Lock()
		if entry, found := resolver.cache[cacheKey]; found && time.Now().Before(entry.expires) {
			resolver.mu.Unlock()
			return entry, nil
		}
		if pending := resolver.inFlight[cacheKey]; pending != nil {
			resolver.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return dnsCacheEntry{}, ctx.Err()
			}
		}
		resolver.inFlight[cacheKey] = make(chan struct{})
		resolver.mu.Unlock()
		break
	}
	entry, err := resolver.query(ctx, host, subnet)
	resolver.mu.Lock()
	if err == nil {
		resolver.cache[cacheKey] = entry
	}
	close(resolver.inFlight[cacheKey])
	delete(resolver.inFlight, cacheKey)
	resolver.mu.Unlock()
	return entry, err
}

func (resolver *dnsResolver) query(ctx context.Context, host, subnet string) (dnsCacheEntry, error) {
	lastErr := errors.New("DoH 未返回公网 IPv4 地址")
	for _, endpoint := range resolver.endpoints {
		if err := ctx.Err(); err != nil {
			return dnsCacheEntry{}, err
		}
		lookupURL, err := url.Parse(endpoint)
		if err != nil {
			return dnsCacheEntry{}, err
		}
		query := lookupURL.Query()
		query.Set("name", host)
		query.Set("type", "A")
		if subnet != "" {
			query.Set("edns_client_subnet", subnet)
		}
		lookupURL.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL.String(), nil)
		if err != nil {
			return dnsCacheEntry{}, err
		}
		request.Header.Set("Accept", "application/dns-json")
		response, err := resolver.client.Do(request)
		if err != nil {
			lastErr = publicError(err)
			continue
		}
		var payload struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				TTL  int    `json:"TTL"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&payload)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || err != nil || payload.Status != 0 {
			lastErr = errors.New("DoH 服务没有返回有效 DNS 记录")
			continue
		}
		entry := dnsCacheEntry{}
		ttl := 300
		var addressErr error
		for _, answer := range payload.Answer {
			if answer.Type != 1 {
				continue
			}
			address := net.ParseIP(answer.Data)
			if address == nil || address.To4() == nil {
				addressErr = errors.New("DoH 返回无效 IPv4 地址")
				break
			}
			entry.addresses = append(entry.addresses, address.String())
			if answer.TTL < ttl {
				ttl = answer.TTL
			}
		}
		if addressErr == nil {
			addressErr = validateCDNAddresses(entry.addresses)
		}
		if addressErr != nil {
			lastErr = fmt.Errorf("DoH 未返回可用公网 IPv4 地址：%w", addressErr)
			continue
		}
		if ttl < 1 {
			ttl = 1
		}
		entry.expires = time.Now().Add(time.Duration(ttl) * time.Second)
		return entry, nil
	}
	sysCtx, sysCancel := context.WithTimeout(ctx, 2*time.Second)
	defer sysCancel()
	ips, sysErr := net.DefaultResolver.LookupIP(sysCtx, "ip4", host)
	if sysErr == nil && len(ips) > 0 {
		var addrs []string
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				addrs = append(addrs, ip4.String())
			}
		}
		if len(addrs) > 0 {
			if valErr := validateCDNAddresses(addrs); valErr == nil {
				return dnsCacheEntry{
					addresses: addrs,
					expires:   time.Now().Add(5 * time.Minute),
				}, nil
			}
		}
	}
	return dnsCacheEntry{}, lastErr
}
