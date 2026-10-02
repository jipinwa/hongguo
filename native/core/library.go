package core

func dramaProvider(drama Drama) string {
	if source := canonicalProviderSource(drama.Source); source != "" {
		return source
	}
	if source, _, ok := splitProviderDramaID(drama.ID); ok {
		return source
	}
	return "cloudfront"
}
