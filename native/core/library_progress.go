package core

import "context"

type libraryProgressKey struct{}

type libraryProgressFunc func(string, []Drama, error, bool)

func reportLibraryProgress(ctx context.Context, source string, items []Drama, err error, done bool) {
	if callback, ok := ctx.Value(libraryProgressKey{}).(libraryProgressFunc); ok && (done || len(items) > 0) {
		callback(source, items, err, done)
	}
}
