package core

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

type nativeCoverNetworkKey struct{}

type nativeCoverRepair struct {
	done    chan struct{}
	address string
	err     error
	retryAt time.Time
}

func (cache *nativeCoverCache) load(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	cache.mu.Lock()
	if repair := cache.repairs[drama.ID]; repair != nil && repair.address != "" {
		drama.Cover = repair.address
	}
	cache.mu.Unlock()
	path, err := cache.loadAddress(ctx, drama, force)
	if err == nil || ctx.Err() != nil {
		return path, err
	}
	address, repairErr := cache.repair(ctx, drama, force)
	if repairErr != nil || address == "" || address == drama.Cover {
		return "", err
	}
	drama.Cover = address
	return cache.loadAddress(ctx, drama, force)
}

func (cache *nativeCoverCache) repair(ctx context.Context, drama nativeDrama, force bool) (string, error) {
	cache.mu.Lock()
	if previous := cache.repairs[drama.ID]; previous != nil {
		if previous.retryAt.IsZero() {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-previous.done:
				return previous.address, previous.err
			}
		}
		if !force && time.Now().Before(previous.retryAt) {
			cache.mu.Unlock()
			return previous.address, previous.err
		}
	}
	if len(cache.repairs) >= 512 {
		for id, entry := range cache.repairs {
			if !entry.retryAt.IsZero() {
				delete(cache.repairs, id)
				break
			}
		}
		if len(cache.repairs) >= 512 {
			cache.mu.Unlock()
			return "", errors.New("海报正在加载，请稍后重试")
		}
	}
	call := &nativeCoverRepair{done: make(chan struct{})}
	cache.repairs[drama.ID] = call
	cache.mu.Unlock()
	work, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var address string
	var err error
	select {
	case cache.repairSlots <- struct{}{}:
		address, err = cache.downloader.nativeCoverAddress(work, drama)
		<-cache.repairSlots
	case <-work.Done():
		err = work.Err()
	}
	cache.mu.Lock()
	call.address, call.err, call.retryAt = address, err, time.Now().Add(5*time.Minute)
	close(call.done)
	cache.mu.Unlock()
	return address, err
}

func (engine *nativeEngine) loadCover(ctx context.Context, drama nativeDrama, force bool) (map[string]any, error) {
	engine.mu.Lock()
	for _, cached := range engine.catalogs[drama.Source] {
		if cached.ID == drama.ID && drama.Cover == "" && cached.Cover != "" {
			drama.Cover = cached.Cover
			break
		}
	}
	engine.mu.Unlock()
	path, err := engine.covers.load(ctx, drama, force)
	if err != nil {
		return nil, err
	}
	engine.covers.mu.Lock()
	address := ""
	if entry := engine.covers.repairs[drama.ID]; entry != nil {
		address = entry.address
	}
	engine.covers.mu.Unlock()
	if address != "" && address != drama.Cover {
		engine.saveCoverAddress(drama.ID, address)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header := make([]byte, 256)
	count, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return map[string]any{"path": path, "heic": isHEICImage(header[:count])}, nil
}

func (engine *nativeEngine) saveCoverAddress(id, address string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	changed := false
	for _, items := range engine.catalogs {
		for index := range items {
			if items[index].ID == id && items[index].Cover != address {
				items[index].Cover = address
				changed = true
			}
		}
	}
	if changed {
		engine.writeCatalogDiskLocked()
	}
}

func (engine *nativeEngine) prepareCover(ctx context.Context, drama nativeDrama) (any, error) {
	result, err := engine.loadCover(ctx, drama, false)
	if err != nil {
		return nil, err
	}
	if result["heic"] != true {
		return result, nil
	}
	file, err := os.Open(result["path"].(string))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, nativeCoverMaxBytes+1))
	file.Close()
	if err != nil {
		return nil, err
	}
	image, err := extractHEICImage(data)
	if err != nil {
		return nil, err
	}
	output, err := os.CreateTemp(engine.covers.directory, ".decode-*.hevc")
	if err != nil {
		return nil, err
	}
	_, writeErr := output.Write(image.data)
	closeErr := output.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(output.Name())
		return nil, errors.Join(writeErr, closeErr)
	}
	result["input"], result["filters"] = output.Name(), strings.Join(image.filters, ",")
	return result, nil
}
