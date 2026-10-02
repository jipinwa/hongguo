package core

import (
	"net/url"

	"strings"
)

func coverPathFromAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return ""
		}
		if strings.HasPrefix(s, "//") {
			return "https:" + s
		}
		if strings.HasPrefix(s, "/api/ui/image") {
			if u, err := url.Parse(s); err == nil {
				if raw := strings.TrimSpace(u.Query().Get("url")); raw != "" {
					return raw
				}
			}
		}
		if u, err := url.Parse(s); err == nil && u.IsAbs() {
			return u.String()
		}
		return strings.TrimLeft(s, "/")
	case map[string]any:
		for _, key := range []string{"url", "src", "path", "cover", "coverUrl", "cover_url", "image", "pic", "poster"} {
			if s := coverPathFromAny(x[key]); s != "" {
				return s
			}
		}
	case []any:
		for _, value := range x {
			if address := coverPathFromAny(value); address != "" {
				return address
			}
		}
	}
	return ""
}

func sourceFromDramaID(id string) string {
	if source, _, ok := splitProviderDramaID(id); ok {
		return source
	}
	return ""
}
