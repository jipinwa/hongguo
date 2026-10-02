package core

import (
	"context"

	"net/http"

	"sync"
)

type playbackResponseURLs struct{ values sync.Map }

type playbackResponseURLsKey struct{}

func rememberPlaybackResponseURL(ctx context.Context, original string, response *http.Response) {
	if urls, _ := ctx.Value(playbackResponseURLsKey{}).(*playbackResponseURLs); urls != nil && response.Request != nil && response.Request.URL != nil {
		urls.values.Store(original, response.Request.URL.String())
	}
}
