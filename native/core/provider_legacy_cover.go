package core

import (
	"net/url"
	"strings"
)

func legacyCoverURL(value any) string {
	address := strings.TrimSpace(coverPathFromAny(value))
	if address == "" {
		return ""
	}
	if strings.HasPrefix(address, "//") {
		address = "https:" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.IsAbs() {
		if validNativeCoverURL(parsed) {
			return address
		}
		return ""
	}
	if strings.Contains(address, "..") || strings.Contains(address, "://") {
		return ""
	}
	address = strings.TrimLeft(address, "/")
	if strings.HasPrefix(address, "upload/") || strings.HasPrefix(address, "upload_01/") {
		return "https://pic.zdmhyg.cn/" + address
	}
	return "https://zzzznnn.lkkwip.cn/" + address
}

func repairLegacyCoverURL(drama nativeDrama) string {
	if sourceFromDramaID(drama.ID) != sourceCloudFront && drama.Source != sourceCloudFront {
		return drama.Cover
	}
	address, err := url.Parse(drama.Cover)
	media, _ := url.Parse(defaultCDNURL)
	if err == nil && strings.EqualFold(address.Hostname(), media.Hostname()) {
		address.Scheme, address.Host = "", ""
		return legacyCoverURL(address.String())
	}
	return legacyCoverURL(drama.Cover)
}
