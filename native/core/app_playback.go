package core

import "context"

func (engine *nativeEngine) nativeBeginPlayback(ctx context.Context, sequence int64) (context.Context, func(), error) {
	engine.playbackMu.Lock()
	defer engine.playbackMu.Unlock()
	if sequence <= engine.playbackSequence {
		return nil, nil, context.Canceled
	}
	if engine.playbackCancel != nil {
		engine.playbackCancel()
	}
	engine.playbackSequence = sequence
	playback, cancel := context.WithCancel(ctx)
	engine.playbackCancel = cancel
	finish := func() {
		cancel()
		engine.playbackMu.Lock()
		if engine.playbackSequence == sequence {
			engine.playbackCancel = nil
		}
		engine.playbackMu.Unlock()
	}
	return playback, finish, nil
}

func (engine *nativeEngine) nativeCancelPlayback(sequence int64) {
	engine.playbackMu.Lock()
	defer engine.playbackMu.Unlock()
	if sequence <= engine.playbackSequence {
		return
	}
	engine.playbackSequence = sequence
	if engine.playbackCancel != nil {
		engine.playbackCancel()
		engine.playbackCancel = nil
	}
}
