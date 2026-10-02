package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestNativeCategoryAliasesAndDetailPersistence(t *testing.T) {
	for _, drama := range []Drama{
		{CategoryName: "都市"}, {CategoryNameSnake: "都市"}, {TypeName: "都市"}, {TypeNameSnake: "都市"},
		{SortName: "都市"}, {SortNameSnake: "都市"}, {Category: "都市"},
	} {
		if nativeNormalize(drama).Category != "都市" {
			t.Fatal("category alias was dropped")
		}
	}
	if nativeNormalize(Drama{ChannelName: "红果"}).Category != "" {
		t.Fatal("source name became a category")
	}
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) { return nil, errors.New("network forbidden") })
	drama := nativeDrama{ID: "hongguo:100", Source: sourceHongguo, Title: "旧名称", Category: "旧分类"}
	engine.catalogs[sourceHongguo] = []nativeDrama{drama}
	engine.catalogs["hongguo|short_play"] = []nativeDrama{drama}
	engine.categoryOptions = map[string][]nativeCategory{sourceHuangguoVideo: {{ID: "2", Name: "合成分类"}}}
	drama.Title, drama.Category = "新名称", "新分类"
	engine.saveDetailMetadata(drama)
	restored := &nativeEngine{directory: engine.directory, catalogs: map[string][]nativeDrama{}, catalogStates: map[string]nativeCatalogState{}}
	restored.loadCatalogCache()
	for _, key := range []string{sourceHongguo, "hongguo|short_play"} {
		if restored.catalogs[key][0].Category != "新分类" || restored.catalogs[key][0].Title != "新名称" {
			t.Fatal("category copy lost metadata")
		}
	}
	if len(restored.categoryOptions[sourceHuangguoVideo]) != 1 {
		t.Fatal("updating metadata erased source categories")
	}
}

func TestNativeRankingPermissionsAndPersistentCache(t *testing.T) {
	for _, board := range rankingBoards {
		err := nativeAuthorizeInput(nativeInput{Action: "rankings", Board: board.ID, Source: sourceHongguo})
		if (err == nil) != nativeSourceAvailable(board.Source) {
			t.Fatal("ranking permission trusts the caller source", board.ID)
		}
	}
	if nativeAuthorizeInput(nativeInput{Action: "rankings", Board: "unknown"}) == nil {
		t.Fatal("unknown board accepted")
	}
	board, _ := findRankingBoard("hongguo-hot")
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(request, 200, rankingFixture(board, 1, rankingRows(1))), nil
	})
	first, err := d.loadRankingPage(context.Background(), board, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	restored := &Downloader{cfg: d.cfg}
	restored.loadRankingCache()
	page, err := restored.loadRankingPage(context.Background(), board, 1, false)
	if err != nil || len(page.Items) != len(first.Items) || page.Items[0].Rank != 1 {
		t.Fatal("ranking cache did not survive restart", err)
	}
}

func syntheticHEIC(width uint32, external bool) []byte {
	box := func(kind string, data ...[]byte) []byte {
		body := bytes.Join(data, nil)
		out := make([]byte, 8)
		binary.BigEndian.PutUint32(out, uint32(8+len(body)))
		copy(out[4:], kind)
		return append(out, body...)
	}
	config := make([]byte, 23)
	config[0], config[21], config[22] = 1, 3, 1
	config = append(config, 32, 0, 1, 0, 2, 0x40, 1)
	size := make([]byte, 12)
	binary.BigEndian.PutUint32(size[4:], width)
	binary.BigEndian.PutUint32(size[8:], 24)
	properties := box("iprp", box("ipco", box("hvcC", config), box("ispe", size), box("irot", []byte{1})), box("ipma", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 3, 0x81, 0x82, 0x83}))
	location := []byte{1, 0, 0, 0, 0x44, 0, 0, 1, 0, 1, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 7}
	if external {
		location[13] = 1
	}
	meta := box("meta", []byte{0, 0, 0, 0}, box("pitm", []byte{0, 0, 0, 0, 0, 1}),
		box("iinf", []byte{0, 0, 0, 0, 0, 1}, box("infe", []byte{2, 0, 0, 0, 0, 1, 0, 0, 'h', 'v', 'c', '1', 0})),
		properties, box("iloc", location), box("idat", []byte{0, 0, 0, 3, 0x26, 1, 0x11}))
	return append(box("ftyp", []byte{'m', 'i', 'f', '1', 0, 0, 0, 0, 'm', 'i', 'f', '1', 'h', 'e', 'i', 'c'}), meta...)
}

func TestNativeHEICPreparationUsesBoundedSyntheticData(t *testing.T) {
	data := syntheticHEIC(16, false)
	parsed, err := extractHEICImage(data)
	want := []byte{0, 0, 0, 1, 0x40, 1, 0, 0, 0, 1, 0x26, 1, 0x11}
	if err != nil || !bytes.Equal(parsed.data, want) || strings.Join(parsed.filters, ",") != "transpose=cclock" {
		t.Fatal("HEIC primary image or rotation was lost", err)
	}
	for _, invalid := range [][]byte{syntheticHEIC(5000, false), syntheticHEIC(16, true), data[:len(data)-1]} {
		if _, err := extractHEICImage(invalid); err == nil {
			t.Fatal("unsafe HEIC container accepted")
		}
	}
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() != "cover.example.test" || request.Context().Value(nativeCoverNetworkKey{}) != true {
			t.Fatal("unexpected cover request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	engine.covers = newNativeCoverCache(engine.directory, engine.downloader)
	result, err := engine.prepareCover(context.Background(), nativeDrama{ID: "hongguo:100", Source: sourceHongguo, Cover: "https://cover.example.test/synthetic.img"})
	if err != nil {
		t.Fatal(err)
	}
	input := result.(map[string]any)["input"].(string)
	stream, err := os.ReadFile(input)
	if err != nil || !bytes.Equal(stream, want) {
		t.Fatal("prepared HEVC was not readable", err)
	}
}

func TestNativeCoverMetadataRejectsOtherDramaAndKeepsCurrentHost(t *testing.T) {
	patch := Drama{ID: "huangguoai:100", Source: sourceHuangguoAI, SourceID: "100"}
	body := `<script type="application/ld+json">{"@type":"VideoObject","url":"/detail/100/","name":"合成剧目","thumbnailUrl":"https://new-cover.example.test/synthetic.jpg"}</script>`
	fresh, err := parseHuangguoSortDetail(body, "https://huangguoai.com/detail/100/", patch)
	if err != nil || nativeNormalize(fresh).Cover != "https://new-cover.example.test/synthetic.jpg" {
		t.Fatal("new image host rejected", err)
	}
	if _, err := parseHuangguoSortDetail(strings.ReplaceAll(body, "/detail/100/", "/detail/101/"), "https://huangguoai.com/detail/100/", patch); err == nil {
		t.Fatal("another drama's cover accepted")
	}
	d := &Downloader{providerHosts: map[string]string{sourceHuangdou: "https://mirror.example.test"}}
	if got := nativeCoverReferer(d, sourceHuangdou, "https://new-cover.example.test/synthetic.img"); got != "https://mirror.example.test/home" {
		t.Fatal("cover referer ignored active source mirror", got)
	}
}
