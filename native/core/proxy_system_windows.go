package core

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func platformSystemProxy() nativeSystemProxy {
	var result nativeSystemProxy
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return result
	}
	defer key.Close()
	pac, _, _ := key.GetStringValue("AutoConfigURL")
	result.PAC = pac != ""
	enabled, _, _ := key.GetIntegerValue("ProxyEnable")
	if enabled == 0 {
		return result
	}
	server, _, _ := key.GetStringValue("ProxyServer")
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	result.Bypass = strings.Split(bypass, ";")
	for _, part := range strings.Split(server, ";") {
		protocol, address, separate := strings.Cut(strings.TrimSpace(part), "=")
		if !separate {
			address, protocol = protocol, ""
		}
		if address == "" {
			continue
		}
		if !strings.Contains(address, "://") {
			prefix := "http://"
			if protocol == "socks" {
				prefix = "socks5://"
			}
			address = prefix + address
		}
		switch protocol {
		case "http":
			result.HTTP = address
		case "https":
			result.HTTPS = address
		case "", "socks":
			if result.HTTP == "" {
				result.HTTP = address
			}
			if result.HTTPS == "" {
				result.HTTPS = address
			}
		}
	}
	return result
}
