package core

import (
	"context"
	"errors"
	"time"
)

func (engine *nativeEngine) nativeDanmaku(ctx context.Context, input nativeInput) (hongguoDanmakuPage, error) {
	if input.StartMS < 0 || input.DurationMS <= input.StartMS || input.DurationMS > danmakuMaxDurationMS {
		return hongguoDanmakuPage{}, errors.New("弹幕时间范围无效")
	}
	engine.mu.Lock()
	choice, exists := engine.playbacks[input.PlaybackSession]
	engine.mu.Unlock()
	if !exists || time.Since(choice.created) > 12*time.Hour {
		return hongguoDanmakuPage{}, errors.New("播放会话已过期")
	}
	if choice.danmakuSeries == "" || choice.danmakuVideo == "" {
		return hongguoDanmakuPage{}, errors.New("本集暂不支持弹幕")
	}
	page, err := engine.downloader.hongguoDanmaku(ctx, choice.danmakuSeries, choice.danmakuVideo, input.StartMS, input.DurationMS)
	if err != nil {
		return hongguoDanmakuPage{}, err
	}
	engine.mu.Lock()
	_, exists = engine.playbacks[input.PlaybackSession]
	engine.mu.Unlock()
	if !exists {
		return hongguoDanmakuPage{}, errors.New("播放会话已过期")
	}
	return page, ctx.Err()
}
