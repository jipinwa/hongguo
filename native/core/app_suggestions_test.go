package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type suggestionTransport func(*http.Request) (*http.Response, error)

func (transport suggestionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestNativeSuggestionsOfficialContract(t *testing.T) {
	engine := &nativeEngine{downloader: &Downloader{
		limiter: newRequestLimiter(1, 0),
		client: &http.Client{Timeout: time.Second, Transport: suggestionTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "hongguoduanju.com" || request.URL.Path != "/incent_resource/suggestion" ||
				request.URL.Query().Get("query") != "永 & 世" || request.URL.Query().Get("app_id") != "8662" ||
				request.URL.Query().Get("count") != "10" || request.Header.Get("Cookie") != "" {
				t.Fatalf("unexpected suggestion request: %s", request.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"data":{"suggest_list":[{"name":"永世长青"},{"name":"永冬之下"},{"name":"永世长青"},{"name":" "}]}}`))}, nil
		})},
	}}
	items, err := engine.suggestions(context.Background(), " 永 & 世 ")
	if err != nil || len(items) != 2 || items[0] != "永世长青" || items[1] != "永冬之下" {
		t.Fatalf("unexpected suggestions: %v %v", items, err)
	}
	if _, err := engine.suggestions(context.Background(), strings.Repeat("永", 101)); err == nil {
		t.Fatal("oversized query accepted")
	}
}

func TestNativeSuggestionsAcceptRootAndEmptyResults(t *testing.T) {
	for _, body := range []string{`{"suggest_list":[]}`, `{"data":{"suggest_list":null}}`} {
		items, err := nativeParseSuggestions([]byte(body))
		if err != nil || len(items) != 0 {
			t.Fatalf("empty results rejected: %v", err)
		}
	}
	items, err := nativeParseSuggestions([]byte(`{"suggest_list":[{"name":"重生"}]}`))
	if err != nil || len(items) != 1 || items[0] != "重生" {
		t.Fatalf("root result: %v %v", items, err)
	}
	if _, err := nativeParseSuggestions([]byte("<html>error</html>")); err == nil {
		t.Fatal("HTML accepted")
	}
}
