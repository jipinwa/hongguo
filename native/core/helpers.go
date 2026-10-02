package core

import (
	"net/url"

	"regexp"

	"strings"
)

var (
	badNameChars = regexp.MustCompile(`[\\/\?%\*:|"<>\x00-\x1f]+`)
	spaceChars   = regexp.MustCompile(`\s+`)
	rePublicURL  = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"'<>]+`)
)

func publicError(err error) error {
	if err == nil {
		return nil
	}
	return redactedError{cause: err}
}

type redactedError struct{ cause error }

func (err redactedError) Error() string { return redactErrorString(err.cause.Error()) }
func (err redactedError) Unwrap() error { return err.cause }

func redactErrorString(s string) string {
	return rePublicURL.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[无效 URL 已隐藏]"
		}
		if u.Host == "" {
			return raw
		}
		if u.User != nil {
			u.User = url.User("redacted")
		}
		if u.RawQuery != "" {
			u.RawQuery = "[redacted]"
		}
		if u.Fragment != "" {
			u.Fragment = "redacted"
		}
		return u.String()
	})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
