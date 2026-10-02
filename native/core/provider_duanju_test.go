package core

import (
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func duanjuFixtureDownloader(t *testing.T, handler http.HandlerFunc) (*Downloader, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	d := &Downloader{
		cfg:           Config{dataDir: t.TempDir(), Retries: 1, PageSize: 30, MaxPagesPerSort: 1},
		client:        server.Client(),
		limiter:       newRequestLimiter(3, 0),
		providerHosts: map[string]string{},
	}
	return d, server
}

func TestDuanjuRegistryKeepsDistinctIdentitiesFromExistingSources(t *testing.T) {
	existing := []string{sourceHongguo, sourceHuangdou, sourceHuangju, sourceYeguo, sourceDSD, sourceHuangguoAI, sourceHuangguoVideo, sourceCloudFront}
	seen := map[string]bool{}
	for _, source := range existing {
		seen[source] = true
	}
	if len(duanjuProviderCatalog) != 12 {
		t.Fatalf("duanju catalog should register 12 sources, got %d", len(duanjuProviderCatalog))
	}
	for _, spec := range duanjuProviderCatalog {
		if seen[spec.ID] {
			t.Fatalf("duanju source %q collides with an existing source id", spec.ID)
		}
		seen[spec.ID] = true
		if canonicalProviderSource(spec.ID) != spec.ID {
			t.Fatalf("duanju source %q is not canonical", spec.ID)
		}
		if !isHuangguoProviderSource(spec.ID) || !isDuanjuProviderSource(spec.ID) {
			t.Fatalf("duanju source %q was not recognized as a provider source", spec.ID)
		}
		if duanjuSourceForHost(duanjuHostOf(spec.Base)) != spec.ID {
			t.Fatalf("duanju source %q does not recognize its own host %q", spec.ID, spec.Base)
		}
	}
	if canonicalProviderSource("星芽") != sourceYaguo || canonicalProviderSource("七猫") != sourceMaoguo {
		t.Fatal("duanju chinese aliases are not canonicalized")
	}
}

func TestDuanjuYaguoParsesCatalogDetailAndSearch(t *testing.T) {
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(request.URL.Path, "/user/v1/account/login"):
			_, _ = writer.Write([]byte(`{"code":"ok","data":{"token":"fixture-token"}}`))
		case strings.Contains(request.URL.Path, "/cloud/v2/theater/home_page"):
			if request.Header.Get("authorization") != "fixture-token" {
				t.Errorf("yaguo catalog did not send the authorization header")
			}
			_, _ = writer.Write([]byte(`{"code":"ok","data":{"total":48,"is_end":false,"list":[{"type":"Theater","theater":{"id":51,"title":"女神的近身高手","descrip":"神医兵王","cover_url":"https://qiniu.example.cn/a.jpg","total":100,"tags":null}}]}}`))
		case strings.Contains(request.URL.Path, "/v2/theater_parent/detail"):
			_, _ = writer.Write([]byte(`{"code":"ok","data":{"id":51,"title":"女神的近身高手","cover_url":"https://qiniu.example.cn/a.jpg","is_over":2,"total":2,"theaters":[{"id":61842,"num":1,"son_title":"第1集","son_video_url":"http://cdn.example.cn/1.mp4"},{"id":61843,"num":2,"son_title":"第2集","son_video_url":"http://cdn.example.cn/2.mp4"}]}}`))
		case strings.Contains(request.URL.Path, "/v3/search"):
			_, _ = writer.Write([]byte(`{"code":"ok","data":{"list":[{"theater":{"id":77,"title":"搜索样本","cover_url":"https://qiniu.example.cn/b.jpg","total":9}}]}}`))
		default:
			http.NotFound(writer, request)
		}
	})
	d.providerHosts[sourceYaguo] = server.URL

	items, more, err := d.fetchYaguoCatalogPage(context.Background(), 1, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("yaguo catalog failed: %+v %v", items, err)
	}
	if items[0].ID != "yaguo:51" || items[0].Source != sourceYaguo || items[0].SourceID != "51" {
		t.Fatalf("yaguo catalog identity is wrong: %+v", items[0])
	}
	if items[0].DisplayTitle() != "女神的近身高手" || !more {
		t.Fatalf("yaguo catalog metadata is wrong: %+v more=%v", items[0], more)
	}

	drama, chapters, err := d.fetchYaguoDetail(context.Background(), "51")
	if err != nil || len(chapters) != 2 {
		t.Fatalf("yaguo detail failed: %+v %v", chapters, err)
	}
	if chapters[0].ID != "yaguo:51:1" || chapters[0].Source != sourceYaguo || chapters[0].VideoURL != "http://cdn.example.cn/1.mp4" {
		t.Fatalf("yaguo chapter is wrong: %+v", chapters[0])
	}
	if got := chapters[0].EpisodeString(1); got != "1" {
		t.Fatalf("yaguo episode number is wrong: %q", got)
	}
	if drama.ReleaseStatus != "finished" {
		t.Fatalf("yaguo release status is wrong: %q", drama.ReleaseStatus)
	}

	found, err := d.searchYaguo(context.Background(), "样本")
	if err != nil || len(found) != 1 || found[0].ID != "yaguo:77" {
		t.Fatalf("yaguo search failed: %+v %v", found, err)
	}
}

func TestDuanjuGuanguoPostsJSONBodyAndReadsEpisodes(t *testing.T) {
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodPost {
			t.Errorf("guanguo catalog must use POST, got %s", request.Method)
		}
		if !strings.Contains(request.URL.Path, "/drama/home/search") {
			http.NotFound(writer, request)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("guanguo body is not JSON: %v", err)
		}
		if payload["pageSize"] != float64(30) {
			t.Errorf("guanguo payload page size is wrong: %+v", payload)
		}
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":[{"title":"九转星辰诀","oneId":"110000042659853849","shortPlayTag":["玄幻仙侠"],"viewCount":658970,"description":"天才少年","horzPoster":"https://pic.cdn.example.cn/a.jpg","episodeCount":81}]}`))
	})
	d.providerHosts[sourceGuanguo] = server.URL

	items, _, err := d.fetchGuanguoCatalogPage(context.Background(), 1, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("guanguo catalog failed: %+v %v", items, err)
	}
	if items[0].ID != "guanguo:110000042659853849" || items[0].EpisodeCount != json.Number("81") {
		t.Fatalf("guanguo catalog is wrong: %+v", items[0])
	}
	if items[0].Views != "65.9万次播放" {
		t.Fatalf("guanguo views text is wrong: %q", items[0].Views)
	}
	if items[0].Cover != "https://pic.cdn.example.cn/a.jpg" {
		t.Fatalf("guanguo cover is wrong: %q", items[0].Cover)
	}
}

func TestDuanjuMaccmsParsesCardsAndPlaylist(t *testing.T) {
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(request.URL.Path, "detail") || strings.Contains(request.URL.Path, "show") {
			_, _ = writer.Write([]byte(`<html><head><title>五五样本 - 在线观看</title><meta name="description" content="简介内容"></head><body>
			<div class="dramaDetail_tagsBox">甜宠 古装</div>
			<ul class="content__playlist playlink clearfix">
			<li><a href="/play/100-1-1.html">第1集</a></li>
			<li><a href="/play/100-1-2.html">第2集</a></li>
			<li><a href="/play/100-1-3.html">APP播放</a></li>
			</ul></body></html>`))
			return
		}
		_, _ = writer.Write([]byte(`<html><body><div class="BrowseList_listBox">
		<div class="BrowseList_listItem"><a href="/detail/100.html" title="五五样本"><img data-original="/pic/100.jpg"></a>
		<span class="meta-post-type2">更新至12集</span></div>
		</div></body></html>`))
	})
	d.providerHosts[sourceWuguo] = server.URL

	items, more, err := d.fetchMaccmsCatalogPage(context.Background(), sourceWuguo, 1, "/type/duanju.html")
	if err != nil || len(items) != 1 || !more {
		t.Fatalf("maccms catalog failed: %+v %v", items, err)
	}
	if items[0].ID != "wuguo:100" || items[0].Source != sourceWuguo {
		t.Fatalf("maccms card identity is wrong: %+v", items[0])
	}
	if items[0].Cover != server.URL+"/pic/100.jpg" {
		t.Fatalf("maccms cover resolution is wrong: %q", items[0].Cover)
	}
	if items[0].EpisodeCount != json.Number("12") {
		t.Fatalf("maccms episode count is wrong: %+v", items[0].EpisodeCount)
	}

	drama, chapters, err := d.fetchMaccmsDetail(context.Background(), sourceWuguo, "100")
	if err != nil || len(chapters) != 2 {
		t.Fatalf("maccms detail failed: %+v %v", chapters, err)
	}
	if chapters[0].VideoURL != server.URL+"/play/100-1-1.html" {
		t.Fatalf("maccms chapter link is wrong: %q", chapters[0].VideoURL)
	}
	if drama.DisplayTitle() != "五五样本" {
		t.Fatalf("maccms title is wrong: %q", drama.DisplayTitle())
	}
}

func TestDuanjuMaccmsPlayerParsingHandlesCommonShells(t *testing.T) {
	playerShell := `<html><script>var player_aaaa={"flag":"1","url":"https:\/\/cdn.example.cn\/hls\/index.m3u8"}</script></html>`
	if got := maccmsNormalizePlaybackURL(maccmsPlayerURL(playerShell)); got != "https://cdn.example.cn/hls/index.m3u8" {
		t.Fatalf("maccms player url parsing is wrong: %q", got)
	}
	encodedShell := `<html><script>var player_aaaa={"url":"https://cdn.example.cn/p.\/u6d4b\u8bd5\/index.m3u8"}</script></html>`
	got := maccmsNormalizePlaybackURL(maccmsPlayerURL(encodedShell))
	if !strings.Contains(got, "index.m3u8") || strings.Contains(got, `\u`) {
		t.Fatalf("maccms encoded folder handling is wrong: %q", got)
	}
	if got := maccmsPlayerURL(`<html>no player here</html>`); got != "" {
		t.Fatalf("maccms should not invent a url: %q", got)
	}
}

func TestDuanjuNiuguoDecryptsResponseWithRequestURIKey(t *testing.T) {
	payload := `{"status":0,"msg":"ok","data":[{"vod_id":575258,"vod_name":"牛果样本","vod_pic":"https://pic.example.cn/a.jpg","vod_remarks":"已完结","vod_douban_score":"4.5"}]}`
	block, err := aes.NewCipher(niuguoDecryptKey("/list?class=&ord"))
	if err != nil {
		t.Fatal(err)
	}
	padded := pkcs7Pad([]byte(payload), aes.BlockSize)
	encrypted := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += aes.BlockSize {
		block.Encrypt(encrypted[offset:offset+aes.BlockSize], padded[offset:offset+aes.BlockSize])
	}
	encoded := base64.StdEncoding.EncodeToString(encrypted)

	decoded, err := niuguoDecryptResponse([]byte(encoded), niuguoDecryptKey("/list?class=&ord"))
	if err != nil {
		t.Fatalf("niuguo decrypt failed: %v", err)
	}
	items := niuguoCatalogItems(decoded, "https://ccc.example.cn")
	if len(items) != 1 || items[0].ID != "niuguo:575258" {
		t.Fatalf("niuguo catalog items are wrong: %+v", items)
	}
	if items[0].DisplayTitle() != "牛果样本" || items[0].Remark != "已完结" {
		t.Fatalf("niuguo drama metadata is wrong: %+v", items[0])
	}
	if _, err := niuguoDecryptResponse([]byte(encoded), niuguoDecryptKey("/list?class=都市&ord")); err == nil {
		t.Fatal("niuguo accepted the wrong request key")
	}
	if _, err := niuguoDecryptResponse([]byte("not base64!!"), niuguoDecryptKey("/list?class=&ord")); err == nil {
		t.Fatal("niuguo accepted invalid base64")
	}
}

func TestDuanjuNiuguoRequestURIUsesEscapedQuery(t *testing.T) {
	address := "https://ccc.example.cn/list?class=&order=%E6%9C%80%E6%96%B0&page=1"
	if got := niuguoRequestURI(address); got != "/list?class=&order=%E6%9C%80%E6%96%B0&page=1" {
		t.Fatalf("niuguo request uri is wrong: %q", got)
	}
	if got := niuguoDecryptKey(niuguoRequestURI(address)); string(got) != "/list?class=&ord" {
		t.Fatalf("niuguo key derivation is wrong: %q", got)
	}
}

func TestDuanjuMaccmsAcceptsHashedClassNamesAndWidenedEpisodes(t *testing.T) {
	document, err := html.Parse(strings.NewReader(`<html><body>
	<ul id="home1" class="_9dbe81752a1dbf17-vodlist clearfix">
	<li class="_9dbe81752a1dbf17-vodlist__item">
	<a class="_9dbe81752a1dbf17-vodlist__thumb lazyload" href="/zywview/20523.html" title="不做替身后被长公主截胡赐婚" data-original="https://pic.example.cn/a.jpg"><span class="pic-text">全集</span></a>
	<h4 class="_9dbe81752a1dbf17-vodlist__title"><a href="/zywview/20523.html" title="不做替身后被长公主截胡赐婚">不做替身后被长..</a></h4>
	</li></ul></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	cards := maccmsCards(document, sourceHuaguo, "https://www.example.cn")
	if len(cards) != 1 || cards[0].SourceID != "20523" {
		t.Fatalf("hashed class cards are wrong: %+v", cards)
	}
	if cards[0].Cover != "https://pic.example.cn/a.jpg" {
		t.Fatalf("hashed class cover is wrong: %q", cards[0].Cover)
	}
	if cards[0].DisplayTitle() != "不做替身后被长公主截胡赐婚" {
		t.Fatalf("hashed class title is wrong: %q", cards[0].DisplayTitle())
	}

	detail, err := html.Parse(strings.NewReader(`<html><body>
	<div class="pcDrama_catalogList">
	<a class="pcDrama_catalogItem" href="/index.php/vod/play/id/45856/sid/1/nid/1.html">第1集</a>
	<a class="pcDrama_catalogItem" href="/index.php/vod/play/id/45856/sid/1/nid/2.html">第2集</a>
	</div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	episodes := maccmsEpisodesFromDocument(detail)
	if len(episodes) != 2 {
		t.Fatalf("catalog item episodes are wrong: %+v", episodes)
	}
}

func TestDuanjuMaccmsDetailCandidatesMatchLiveSites(t *testing.T) {
	cases := map[string]string{
		sourceWuguo:   "https://www.example.cn/index.php/vod/detail/id/45856.html",
		sourceWangguo: "https://www.example.cn/vod/48478.html",
		sourceHuaguo:  "https://www.example.cn/zywview/20523.html",
	}
	for source, want := range cases {
		got := maccmsDetailCandidates(source, "https://www.example.cn", firstNumericTail(want))
		if len(got) == 0 || got[0] != want {
			t.Fatalf("%s detail candidate is wrong: %v", source, got)
		}
	}
}

func firstNumericTail(address string) string {
	cleaned := strings.TrimSuffix(address, ".html")
	parts := strings.Split(cleaned, "/")
	for index := len(parts) - 1; index >= 0; index-- {
		if webProviderNumericID.MatchString(parts[index]) {
			return parts[index]
		}
	}
	return ""
}

func TestDuanjuHeguoParsesNextDataWrappedResponses(t *testing.T) {
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(request.URL.Path, "/drama/") {
			_, _ = writer.Write([]byte(`<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"bookInfoVo":{"bookId":9001,"title":"河马样本","coverWap":"https://pic.example.cn/a.jpg","introduction":"简介","statusDesc":"已完结","totalChapterNum":2,"categoryList":[{"name":"甜宠"}]},"chapterList":[{"chapterId":"c1","chapterName":"第1集"},{"chapterId":"c2","chapterName":"第2集"}]}}}</script></html>`))
			return
		}
		_, _ = writer.Write([]byte(`<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"page":1,"pages":3,"bookList":[{"bookId":9001,"bookName":"河马样本","coverWap":"https://pic.example.cn/a.jpg","statusDesc":"连载中","totalChapterNum":30}]}}}</script></html>`))
	})
	d.providerHosts[sourceHeguo] = server.URL

	items, more, err := d.fetchHeguoCatalogPage(context.Background(), 1, "462")
	if err != nil || len(items) != 1 || !more {
		t.Fatalf("heguo catalog failed: %+v %v", items, err)
	}
	if items[0].ID != "heguo:9001" || items[0].Source != sourceHeguo {
		t.Fatalf("heguo identity is wrong: %+v", items[0])
	}
	drama, chapters, err := d.fetchHeguoDetail(context.Background(), "9001")
	if err != nil || len(chapters) != 2 {
		t.Fatalf("heguo detail failed: %+v %v", chapters, err)
	}
	if chapters[0].PageURL == "" || chapters[0].VideoURL != "" {
		t.Fatalf("heguo chapters must defer media resolution: %+v", chapters[0])
	}
	if drama.ReleaseStatus != "finished" || drama.EpisodeCount != json.Number("2") {
		t.Fatalf("heguo drama metadata is wrong: %+v", drama)
	}
}

func TestDuanjuSearchSupportMatchesReferenceScripts(t *testing.T) {
	supported := []string{sourceYaguo, sourceMaoguo, sourceFanguo, sourceGuanguo, sourceHeguo, sourceXingguo, sourceHuaguo, sourceNiuguo, sourceWangguo, sourceFaguo, sourcePiguo, sourceWuguo}
	for _, source := range supported {
		if !duanjuSupportsSearch(source) {
			t.Fatalf("%s should support online search", source)
		}
	}
	if duanjuSupportsSearch("shuangguo") || duanjuSupportsSearch("muguo") {
		t.Fatal("removed sources must not advertise search")
	}
	if duanjuSupportsSearch(sourceHongguo) {
		t.Fatal("duanju search support leaked onto an existing source")
	}
	for _, source := range []string{sourceYaguo, sourceGuanguo, sourceHuaguo, sourceNiuguo, sourceWangguo, sourceFaguo, sourcePiguo, sourceWuguo} {
		if !duanjuSupportsPaging(source) {
			t.Fatalf("%s should support paging", source)
		}
	}
	for _, source := range []string{sourceHeguo, sourceXingguo, sourceMaoguo, sourceFanguo} {
		if duanjuSupportsPaging(source) {
			t.Fatalf("%s reference script is single page", source)
		}
	}
}

func TestDuanjuMaoguoSearchSignatureFollowsReferenceOrder(t *testing.T) {
	want := duanjuMD5("extend=page=1read_preference=0track_id=" + duanjuMaoguoTrackID + "wd=都市" + duanjuMaoguoKey)
	if got := duanjuMD5("extend=page=1read_preference=0track_id=" + duanjuMaoguoTrackID + "wd=都市" + duanjuMaoguoKey); got != want {
		t.Fatalf("maoguo signature is unstable: %q", got)
	}
	if want == duanjuMD5("extend=page=1read_preference=0track_id="+duanjuMaoguoTrackID+duanjuMaoguoKey+"wd=都市") {
		t.Fatal("maoguo signature must place the key after the keyword")
	}
}

func TestDuanjuMaccmsFallsBackToDetailAnchors(t *testing.T) {
	document, err := html.Parse(strings.NewReader(`<html><body>
	<div class="random-hash-outer"><ul><li class="x1 y2 z3"><div class="q9 w8 e7 thumb">
	<a class="a1 b2 v-thumb stui-vodlist__thumb lazyload" href="/xzyxvd/269321.html" title="都市古仙医" data-original="https://image.example.cn/a.jpg">
	<span class="c3 pic-text text-right">第216集</span></a></div></li></ul></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	cards := maccmsCards(document, sourceFaguo, "https://www.example.cn")
	if len(cards) != 1 || cards[0].SourceID != "269321" {
		t.Fatalf("anchor fallback cards are wrong: %+v", cards)
	}
	if cards[0].DisplayTitle() != "都市古仙医" || cards[0].Remark != "第216集" {
		t.Fatalf("anchor fallback metadata is wrong: %+v", cards[0])
	}
	if cards[0].Cover != "https://image.example.cn/a.jpg" {
		t.Fatalf("anchor fallback cover is wrong: %q", cards[0].Cover)
	}
}

func TestDuanjuRejectsMismatchedSourceIDsAndCategories(t *testing.T) {
	if split, id, ok := splitProviderDramaID("yaguo:51"); !ok || split != sourceYaguo || id != "51" {
		t.Fatalf("duanju drama id split failed: %q %q %v", split, id, ok)
	}
	if _, _, ok := splitProviderDramaID("yaguo:"); ok {
		t.Fatal("duanju accepted an empty source id")
	}
	if split, _, ok := splitProviderDramaID("unknown:1"); ok {
		t.Fatalf("duanju accepted an unknown source: %q", split)
	}
	if validDuanjuCategory(sourceGuanguo, "../etc") || validDuanjuCategory(sourceMaoguo, "a|b") {
		t.Fatal("duanju accepted an invalid category")
	}
	if !validDuanjuCategory(sourceGuanguo, "462") || !validDuanjuCategory(sourceHeguo, "417-464") {
		t.Fatal("duanju rejected a valid numeric category")
	}
	if !validDuanjuCategory(sourceFanguo, "都市") {
		t.Fatal("fanguo should accept a chinese category")
	}
}

func TestDuanjuSourceAvailabilityFollowsBuildVariant(t *testing.T) {
	previous := buildAllSources
	buildAllSources = "false"
	if nativeSourceAvailable(sourceYaguo) {
		t.Fatal("hongguo-only build must not expose duanju sources")
	}
	buildAllSources = "true"
	if !nativeSourceAvailable(sourceYaguo) || !nativeSourceAvailable(sourceWuguo) {
		t.Fatal("all-sources build must expose duanju sources")
	}
	buildAllSources = previous
}

func TestDuanjuNormalizePlaybackURLStripsTrailingPunctuation(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.cn/wjv11/202508/15/SnEXGkSedD83/video/index.m3u8,": "https://cdn.example.cn/wjv11/202508/15/SnEXGkSedD83/video/index.m3u8",
		"https://cdn.example.cn/a/index.m3u8。":                                  "https://cdn.example.cn/a/index.m3u8",
		"https://cdn.example.cn/a/index.m3u8":                                   "https://cdn.example.cn/a/index.m3u8",
		`https:\/\/cdn.example.cn\/a\/index.m3u8\,`:                             "https://cdn.example.cn/a/index.m3u8",
	}
	for input, want := range cases {
		if got := maccmsNormalizePlaybackURL(input); got != want {
			t.Fatalf("normalize %q => %q, want %q", input, got, want)
		}
	}
	if got := maccmsNormalizePlaybackURL(","); got != "" {
		t.Fatalf("punctuation-only input should normalize to empty, got %q", got)
	}
}

func TestDuanjuNiuguoResolverRejectsMissingEpisodeID(t *testing.T) {
	d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(2, 0), providerHosts: map[string]string{}}
	_, err := d.resolveNiuguoMedia(context.Background(), Task{Chapter: Chapter{Source: sourceNiuguo}})
	if err == nil {
		t.Fatal("niuguo resolver accepted an empty episode id")
	}
	if !strings.Contains(err.Error(), "解析 ID") {
		t.Fatalf("niuguo resolver error is unhelpful: %v", err)
	}
}

func TestDuanjuGuanguoEmptyDetailIsReportedAsUpstream(t *testing.T) {
	d, _ := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, "shortVideoDetail") {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = writer.Write([]byte(`{"code":200,"msg":"success","data":null,"title":null}`))
	})
	_, _, err := d.fetchGuanguoDetail(context.Background(), "110000042659853849")
	if err == nil {
		t.Fatal("guanguo empty detail should fail")
	}
	if !strings.Contains(err.Error(), "返回空数据") {
		t.Fatalf("guanguo empty detail error should blame upstream, got %v", err)
	}
}

func TestDuanjuCleanTitleStripsLiveSeoSuffixes(t *testing.T) {
	cases := map[string]string{
		"黑月光归来-短剧黑月光归来全集-黑月光归来免费在线观看 - 免费五五短剧视频分享大全 - 最新的免费短剧视频": "黑月光归来",
		"修仙入世我在都市当靠山_高清完整版在线观看_爽文短剧 - 短剧网":                       "修仙入世我在都市当靠山",
		"三万月薪，我自己做主-短剧三万月薪，我自己做主全集-三万月薪，我自己做主免费在线观看":             "三万月薪，我自己做主",
		"不做替身后被长公主截胡赐婚 - 花生短剧":                                   "不做替身后被长公主截胡赐婚",
		"九转星辰诀":   "九转星辰诀",
		"我的剧-第二季": "我的剧-第二季",
	}
	for input, want := range cases {
		if got := maccmsCleanTitle(input); got != want {
			t.Fatalf("clean %q => %q, want %q", input, got, want)
		}
	}
}

func TestDuanjuSearchCoverageMatchesImplementedSources(t *testing.T) {
	for _, spec := range duanjuProviderCatalog {
		if !spec.Searcher {
			t.Fatalf("%s must advertise online search", spec.ID)
		}
		if !duanjuSupportsSearch(spec.ID) {
			t.Fatalf("%s should report search support", spec.ID)
		}
	}
	if !duanjuSupportsSearch(sourceHeguo) || !duanjuSupportsSearch(sourceXingguo) {
		t.Fatal("heguo and xingguo search must be advertised")
	}
}

func TestDuanjuHeguoSearchBuildsDocumentedRequest(t *testing.T) {
	var gotPath, gotMethod, gotPName, gotBody string
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		gotPath, gotMethod, gotPName = request.URL.Path, request.Method, request.Header.Get("pname")
		payload, _ := io.ReadAll(request.Body)
		gotBody = string(payload)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"retCode":0,"data":{"isMore":1,"totalSize":1,"bookList":[{"bookId":"41000131810","bookName":"雪夜逃婚","coverWap":"https://seoali.zqkanshu.com/a.jpg","totalChapterNum":"28","statusDesc":"完本"}]}}`))
	})
	d.providerHosts[sourceHeguo] = server.URL

	items, err := d.searchHeguo(context.Background(), "都市")
	if err != nil || len(items) != 1 {
		t.Fatalf("heguo search failed: %+v %v", items, err)
	}
	if gotPath != heguoSearchPath || gotMethod != http.MethodPost || gotPName != heguoSearchPName {
		t.Fatalf("heguo search request is wrong: %s %s pname=%s", gotMethod, gotPath, gotPName)
	}
	var sent map[string]any
	if json.Unmarshal([]byte(gotBody), &sent) != nil {
		t.Fatalf("heguo search body is not json: %q", gotBody)
	}
	if sent["keyword"] != "都市" {
		t.Fatalf("heguo search keyword is wrong: %v", sent)
	}
	if index, _ := sent["index"].(float64); int(index) != 1 {
		t.Fatalf("heguo search index is wrong: %v", sent["index"])
	}
	if sourceType, _ := sent["sourceType"].(float64); int(sourceType) != heguoSearchSourceType {
		t.Fatalf("heguo search sourceType is wrong: %v", sent["sourceType"])
	}
	if items[0].Title != "雪夜逃婚" || items[0].SourceID != "41000131810" {
		t.Fatalf("heguo search item is wrong: %+v", items[0])
	}
}

func TestDuanjuXingguoSearchUsesKeyParameter(t *testing.T) {
	var gotQuery url.Values
	d, server := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {
		gotQuery = request.URL.Query()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":"0","data":{"page":1,"datalist":[{"id":"461565999316993","name":"都市联盟","icon":"http://img.novel.wsljf.xyz/a.jpg","chapterCount":38,"introduction":"简介"}]}}`))
	})
	d.providerHosts[sourceXingguo] = server.URL

	items, err := d.searchXingguo(context.Background(), "都市")
	if err != nil || len(items) != 1 {
		t.Fatalf("xingguo search failed: %+v %v", items, err)
	}
	if gotQuery.Get("key") != "都市" {
		t.Fatalf("xingguo search must use the key parameter, got %q", gotQuery.Encode())
	}
	if gotQuery.Get("token") != xingguoToken || gotQuery.Get("pageSize") == "" {
		t.Fatalf("xingguo search dropped required parameters: %q", gotQuery.Encode())
	}
	if items[0].Title != "都市联盟" || !strings.HasPrefix(items[0].SourceID, "461565999316993") {
		t.Fatalf("xingguo search item is wrong: %+v", items[0])
	}
}

func TestDuanjuSearchRejectsBlankKeyword(t *testing.T) {
	d, _ := duanjuFixtureDownloader(t, func(writer http.ResponseWriter, request *http.Request) {})
	if _, err := d.searchHeguo(context.Background(), "   "); err == nil {
		t.Fatal("heguo search must reject a blank keyword")
	}
	if _, err := d.searchXingguo(context.Background(), "   "); err == nil {
		t.Fatal("xingguo search must reject a blank keyword")
	}
}

func TestDuanjuPlainTextStripsSearchHighlightMarkup(t *testing.T) {
	cases := map[string]string{
		"<font color='#ff4242'>都</font><font color='#ff4242'>市</font>无上仙尊（<font color='#ff4242'>都</font><font color='#ff4242'>市</font>之无双仙尊）": "都市无上仙尊（都市之无双仙尊）",
		"<em>都市</em>风云":     "都市风云",
		"普通标题":              "普通标题",
		"":                  "",
		"  <b>加粗</b>  空格  ": "加粗 空格",
	}
	for input, want := range cases {
		if got := duanjuPlainText(input); got != want {
			t.Fatalf("plain %q => %q, want %q", input, got, want)
		}
	}
}
