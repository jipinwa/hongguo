package core

import (
	"context"
	"errors"
	"strings"
	"time"
)

type nativeReadRequest struct {
	sequence int64
	cancel   context.CancelFunc
	updated  time.Time
}

func validNativeRead(input nativeInput) bool {
	return input.Sequence > 0 && len(input.Session) > 0 && len(input.Session) <= 128 &&
		strings.IndexFunc(input.Session, func(r rune) bool {
			return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("-_:.", r))
		}) < 0
}

func (engine *nativeEngine) beginRead(ctx context.Context, input nativeInput) (context.Context, func(), error) {
	if !validNativeRead(input) {
		return nil, nil, errors.New("请求标记无效")
	}
	engine.readMu.Lock()
	defer engine.readMu.Unlock()
	engine.prepareReadsLocked()
	previous := engine.reads[input.Session]
	if input.Sequence <= previous.sequence {
		return nil, nil, context.Canceled
	}
	if previous.cancel != nil {
		previous.cancel()
	}
	if engine.readCount >= 16 || len(engine.reads) >= 128 && previous.sequence == 0 {
		return nil, nil, errors.New("正在处理的请求较多，请稍后重试")
	}
	work, cancel := context.WithCancel(ctx)
	engine.reads[input.Session] = nativeReadRequest{sequence: input.Sequence, cancel: cancel, updated: time.Now()}
	engine.readCount++
	finish := func() {
		cancel()
		engine.readMu.Lock()
		defer engine.readMu.Unlock()
		engine.readCount--
		if request := engine.reads[input.Session]; request.sequence == input.Sequence {
			request.cancel, request.updated = nil, time.Now()
			engine.reads[input.Session] = request
		}
	}
	return work, finish, nil
}

func (engine *nativeEngine) prepareReadsLocked() {
	if engine.reads == nil {
		engine.reads = map[string]nativeReadRequest{}
	}
	for key, request := range engine.reads {
		if request.cancel == nil && time.Since(request.updated) > 15*time.Minute {
			delete(engine.reads, key)
		}
	}
}

func (engine *nativeEngine) cancelRead(input nativeInput) {
	if !validNativeRead(input) {
		return
	}
	engine.readMu.Lock()
	defer engine.readMu.Unlock()
	engine.prepareReadsLocked()
	previous := engine.reads[input.Session]
	if input.Sequence < previous.sequence {
		return
	}
	if previous.cancel != nil {
		previous.cancel()
	}
	if len(engine.reads) < 128 || previous.sequence > 0 {
		engine.reads[input.Session] = nativeReadRequest{sequence: input.Sequence, updated: time.Now()}
	}
}
