package core

import (
	"context"
	"errors"
	"time"
)

type nativeRecommendationState struct {
	Query   hongguoRecommendationQuery `json:"query"`
	Page    int                        `json:"page"`
	HasMore bool                       `json:"hasMore"`
}

func nativeRecommendationKey(genre string) string { return sourceHongguo + "|recommendation:" + genre }

func (engine *nativeEngine) cachedRecommendations(genre string) (nativeCatalogResult, error) {
	if err := (hongguoRecommendationQuery{Genre: genre}).validate(); err != nil {
		return nativeCatalogResult{}, err
	}
	result := engine.nativeCached(nativeRecommendationKey(genre))
	engine.mu.Lock()
	_, found := engine.recommendations[genre]
	engine.mu.Unlock()
	if !found && len(result.Items) > 0 {
		result.Fresh, result.HasMore = false, false
		result.Warning = joinNativeWarnings(result.Warning, "推荐位置已失效，请刷新推荐")
	}
	return result, nil
}

func (engine *nativeEngine) nativeRecommendations(ctx context.Context, input nativeInput) (nativeCatalogResult, error) {
	genre := input.Category
	if err := (hongguoRecommendationQuery{Genre: genre}).validate(); err != nil {
		return nativeCatalogResult{}, err
	}
	unlock, err := engine.lockSourceCatalog(ctx, sourceHongguo)
	if err != nil {
		return nativeCatalogResult{}, err
	}
	defer unlock()
	if retried, err := engine.retryCatalogSave(); retried {
		cached, _ := engine.cachedRecommendations(genre)
		if err != nil || len(cached.Items) > 0 {
			return cached, nil
		}
	}
	cached, _ := engine.cachedRecommendations(genre)
	engine.mu.Lock()
	state, found := engine.recommendations[genre]
	engine.mu.Unlock()
	if !input.Force && found && len(cached.Items) > 0 && (input.Command != "more" || !state.HasMore) {
		return cached, nil
	}
	if !input.Force && !found && len(cached.Items) > 0 {
		return cached, nil
	}
	if input.Force || !found {
		state = nativeRecommendationState{Query: hongguoRecommendationQuery{Genre: genre}, HasMore: true}
	}
	page, err := engine.downloader.fetchHongguoRecommendations(ctx, state.Query)
	if err != nil {
		if len(cached.Items) == 0 || errors.Is(err, context.Canceled) {
			return nativeCatalogResult{}, err
		}
		cached.Fresh = false
		cached.Warning = joinNativeWarnings(cached.Warning, publicError(err).Error())
		return cached, nil
	}
	if err := ctx.Err(); err != nil {
		return nativeCatalogResult{}, err
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.recommendations == nil {
		engine.recommendations = map[string]nativeRecommendationState{}
	}
	key := nativeRecommendationKey(genre)
	previous := engine.catalogs[key]
	if input.Force {
		previous = nil
	}
	seen := make(map[string]bool, len(previous))
	for _, drama := range previous {
		seen[drama.ID] = true
	}
	fresh := make([]nativeDrama, 0, len(page.Dramas))
	known := make(map[string]nativeDrama, len(engine.catalogs[sourceHongguo]))
	for _, drama := range engine.catalogs[sourceHongguo] {
		known[drama.ID] = drama
	}
	for _, drama := range page.Dramas {
		item := nativeNormalize(drama)
		if old, exists := known[item.ID]; exists {
			item = mergeNativeDrama(old, item)
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 && page.HasMore {
		return nativeCatalogResult{}, errors.New("暂时没有新的推荐，已保留上次位置；可重试或刷新推荐")
	}
	items := mergeNativeCatalog(previous, fresh)
	identifiers := make([]string, 0, min(540, len(items)))
	for _, drama := range items[max(0, len(items)-540):] {
		identifiers = append(identifiers, drama.ID)
	}
	session := page.SessionID
	if session == "" {
		session = state.Query.SessionID
	}
	state.Query = hongguoRecommendationQuery{Genre: genre, Offset: page.NextOffset, SessionID: session, Seen: identifiers}
	state.Page++
	state.HasMore = page.HasMore
	engine.recommendations[genre] = state
	engine.catalogs[key] = items
	engine.catalogs[sourceHongguo] = mergeNativeCatalog(engine.catalogs[sourceHongguo], fresh)
	engine.hongguoCatalog = engine.downloader.hongguoCatalogSnapshot()
	engine.catalogStates[key] = nativeCatalogState{UpdatedAt: time.Now(), Page: state.Page, HasMore: state.HasMore}
	result := nativeCatalogResult{Items: append([]nativeDrama{}, items...), Page: state.Page, HasMore: state.HasMore, Fresh: true}
	if err := engine.writeCatalogDiskLocked(); err != nil {
		result.Fresh, result.Warning, result.saveError = false, err.Error(), err
	}
	return result, nil
}
