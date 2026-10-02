package core

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	sourceYaguo   = "yaguo"
	sourceMaoguo  = "maoguo"
	sourceFanguo  = "fanguo"
	sourceGuanguo = "guanguo"
	sourceHeguo   = "heguo"
	sourceXingguo = "xingguo"
	sourceHuaguo  = "huaguo"
	sourceNiuguo  = "niuguo"
	sourceWangguo = "wangguo"
	sourceFaguo   = "faguo"
	sourcePiguo   = "piguo"
	sourceWuguo   = "wuguo"

	duanjuMaxBodyBytes = 8 * 1024 * 1024
	duanjuUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	yaguoBaseURL    = "https://app.whjzjx.cn"
	yaguoLoginURL   = "https://u.shytkjgs.com/user/v1/account/login"
	maoguoBaseURL   = "https://api-store.qmplaylet.com"
	maoguoReadURL   = "https://api-read.qmplaylet.com"
	fanguoBaseURL   = "https://xifan-api-cn.youlishipin.com"
	guanguoBaseURL  = "https://api.drama.9ddm.com"
	heguoBaseURL    = "https://www.kuaikaw.cn"
	xingguoBaseURL  = "http://read.api.duodutek.com"
	huaguoBaseURL   = "https://www.zywest263.com"
	niuguoBaseURL   = "https://ccc.chaojichaojichanga.com:35620"
	niuguoParseURL  = "http://ccs.jshh.gzbaoxian.com"
	niuguoParseURL2 = "http://101.42.92.211:5560"
	wangguoBaseURL  = "https://www.duanju2.com"
	faguoBaseURL    = "https://www.xzyx168.com"
	piguoBaseURL    = "https://ptt.red"
	wuguoBaseURL    = "https://www.duanju55.com"
)

type duanjuSourceSpec struct {
	ID       string
	Name     string
	Base     string
	Kind     string
	Agent    string
	Referer  string
	Searcher bool
	Paged    bool
	Browser  bool
}

var duanjuProviderCatalog = []duanjuSourceSpec{
	{ID: sourceYaguo, Name: "芽果", Base: yaguoBaseURL, Kind: "api", Searcher: true, Paged: true},
	{ID: sourceMaoguo, Name: "猫果", Base: maoguoBaseURL, Kind: "api", Searcher: true},
	{ID: sourceFanguo, Name: "饭果", Base: fanguoBaseURL, Kind: "api", Searcher: true},
	{ID: sourceGuanguo, Name: "观果", Base: guanguoBaseURL, Kind: "api", Agent: "okhttp/5.1.0", Searcher: true, Paged: true},
	{ID: sourceHeguo, Name: "河果", Base: heguoBaseURL, Kind: "next", Searcher: true},
	{ID: sourceXingguo, Name: "星果", Base: xingguoBaseURL, Kind: "api", Searcher: true},
	{ID: sourceHuaguo, Name: "花果", Base: huaguoBaseURL, Kind: "maccms", Searcher: true, Paged: true},
	{ID: sourceNiuguo, Name: "牛果", Base: niuguoBaseURL, Kind: "api", Searcher: true, Paged: true},
	{ID: sourceWangguo, Name: "网果", Base: wangguoBaseURL, Kind: "maccms", Searcher: true, Paged: true},
	{ID: sourceFaguo, Name: "发果", Base: faguoBaseURL, Kind: "maccms", Searcher: true, Paged: true},
	{ID: sourcePiguo, Name: "皮果", Base: piguoBaseURL, Kind: "maccms", Searcher: true, Paged: true, Browser: true},
	{ID: sourceWuguo, Name: "伍果", Base: wuguoBaseURL, Kind: "maccms", Searcher: true, Paged: true},
}

var duanjuMarkupPattern = regexp.MustCompile(`<[^>]{1,80}>`)

func duanjuPlainText(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.Contains(text, "<") {
		return text
	}
	text = duanjuMarkupPattern.ReplaceAllString(text, "")
	text = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

const (
	heguoSearchPath       = "/seo/video/6007"
	heguoSearchPName      = "www.kuaikaw.cn"
	heguoSearchSourceType = 1
)

var duanjuSourceAliases = map[string]string{
	"yaguo": "yaguo", "xingya": sourceYaguo, "星芽": sourceYaguo, "app.whjzjx.cn": sourceYaguo,
	"maoguo": "maoguo", "qimao": sourceMaoguo, "七猫": sourceMaoguo, "api-store.qmplaylet.com": sourceMaoguo,
	"fanguo": "fanguo", "xifan": sourceFanguo, "西饭": sourceFanguo, "xifan-api-cn.youlishipin.com": sourceFanguo,
	"guanguo": "guanguo", "weiguan": sourceGuanguo, "围观": sourceGuanguo, "api.drama.9ddm.com": sourceGuanguo,
	"heguo": "heguo", "hema": sourceHeguo, "河马": sourceHeguo, "www.kuaikaw.cn": sourceHeguo, "kuaikaw.cn": sourceHeguo,
	"xingguo": "xingguo", "xingxing": sourceXingguo, "星星": sourceXingguo, "read.api.duodutek.com": sourceXingguo,
	"huaguo": "huaguo", "huasheng": sourceHuaguo, "花生": sourceHuaguo, "www.zywest263.com": sourceHuaguo,
	"niuguo": "niuguo", "niuniu": sourceNiuguo, "牛牛": sourceNiuguo,
	"wangguo": "wangguo", "duanjuwangzhan": sourceWangguo, "短剧网站": sourceWangguo, "www.duanju2.com": sourceWangguo,
	"faguo": "faguo", "168": sourceFaguo, "xzyx168": sourceFaguo, "www.xzyx168.com": sourceFaguo,
	"piguo": "piguo", "ptt": sourcePiguo, "ptt.red": sourcePiguo,
	"wuguo": "wuguo", "wuwu": sourceWuguo, "五五": sourceWuguo, "www.duanju55.com": sourceWuguo,
}

var duanjuSourcesByName = func() map[string]duanjuSourceSpec {
	registry := map[string]duanjuSourceSpec{}
	for _, spec := range duanjuProviderCatalog {
		registry[spec.ID] = spec
	}
	return registry
}()

func isDuanjuProviderSource(source string) bool {
	_, found := duanjuSourcesByName[canonicalProviderSource(source)]
	return found
}

func duanjuSourceSpecFor(source string) (duanjuSourceSpec, bool) {
	spec, found := duanjuSourcesByName[canonicalProviderSource(source)]
	return spec, found
}

func duanjuSourceName(source string) string {
	if spec, found := duanjuSourceSpecFor(source); found {
		return spec.Name
	}
	return canonicalProviderSource(source)
}

func (d *Downloader) duanjuBaseURL(source string) string {
	spec, found := duanjuSourceSpecFor(source)
	if !found {
		return ""
	}
	if configured := d.providerHosts[canonicalProviderSource(source)]; configured != "" {
		return strings.TrimRight(configured, "/")
	}
	return strings.TrimRight(spec.Base, "/")
}

func duanjuSourceForHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "app.whjzjx.cn" || host == "u.shytkjgs.com":
		return sourceYaguo
	case host == "api-store.qmplaylet.com" || host == "api-read.qmplaylet.com":
		return sourceMaoguo
	case host == "xifan-api-cn.youlishipin.com":
		return sourceFanguo
	case host == "api.drama.9ddm.com":
		return sourceGuanguo
	case host == "www.kuaikaw.cn" || host == "kuaikaw.cn":
		return sourceHeguo
	case host == "read.api.duodutek.com":
		return sourceXingguo
	case host == "www.zywest263.com" || host == "zywest263.com":
		return sourceHuaguo
	case host == "ccc.chaojichaojichanga.com":
		return sourceNiuguo
	case host == "www.duanju2.com" || host == "duanju2.com":
		return sourceWangguo
	case host == "www.xzyx168.com" || host == "xzyx168.com":
		return sourceFaguo
	case host == "ptt.red" || host == "www.ptt.red":
		return sourcePiguo
	case host == "www.duanju55.com" || host == "duanju55.com":
		return sourceWuguo
	default:
		return ""
	}
}

func validDuanjuCategory(source, category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 128 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	switch canonicalProviderSource(source) {
	case sourceHeguo, sourceXingguo, sourceYaguo, sourceMaoguo, sourceNiuguo:
		return webProviderNumericID.MatchString(strings.SplitN(category, "-", 2)[0])
	case sourceFanguo, sourceGuanguo:
		return true
	default:
		return true
	}
}

type duanjuCategory struct {
	ID   string
	Name string
}

var duanjuStaticCategories = map[string][]duanjuCategory{
	sourceYaguo: {
		{ID: "1", Name: "剧场"},
		{ID: "9", Name: "榜单"},
	},
	sourceMaoguo: {
		{ID: "0", Name: "推荐"},
	},
	sourceFanguo: {
		{ID: "都市", Name: "都市"},
		{ID: "甜宠", Name: "甜宠"},
		{ID: "逆袭", Name: "逆袭"},
		{ID: "战神", Name: "战神"},
		{ID: "古装", Name: "古装"},
		{ID: "穿越", Name: "穿越"},
		{ID: "萌宝", Name: "萌宝"},
	},
	sourceXingguo: {
		{ID: "1287", Name: "推荐一"},
		{ID: "1288", Name: "推荐二"},
		{ID: "1289", Name: "推荐三"},
		{ID: "1290", Name: "推荐四"},
		{ID: "1291", Name: "推荐五"},
	},
	sourceNiuguo: {
		{ID: "1", Name: "短剧"},
		{ID: "2", Name: "电影"},
		{ID: "3", Name: "电视剧"},
		{ID: "4", Name: "动漫"},
		{ID: "5", Name: "综艺"},
	},
	sourceHeguo: {
		{ID: "462", Name: "甜宠"},
		{ID: "1102", Name: "古装仙侠"},
		{ID: "1145", Name: "现代言情"},
		{ID: "1170", Name: "青春"},
		{ID: "585", Name: "豪门恩怨"},
		{ID: "417-464", Name: "逆袭"},
		{ID: "439-465", Name: "重生"},
		{ID: "1159", Name: "系统"},
		{ID: "1147", Name: "总裁"},
		{ID: "943", Name: "职场商战"},
	},
	sourcePiguo: {
		{ID: "67", Name: "爽剧"},
		{ID: "68", Name: "言情"},
		{ID: "70", Name: "穿越"},
		{ID: "71", Name: "悬疑"},
		{ID: "73", Name: "古装"},
		{ID: "80", Name: "都市"},
		{ID: "84", Name: "甜宠"},
		{ID: "85", Name: "恋爱"},
		{ID: "74", Name: "其他"},
	},
	sourceHuaguo: {
		{ID: "27", Name: "短剧"},
	},
}

func duanjuQueryEscape(value string) string { return url.QueryEscape(strings.TrimSpace(value)) }

func duanjuAbsolute(base, reference string) string {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return ""
	}
	if strings.HasPrefix(reference, "//") {
		return "https:" + reference
	}
	address, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	if address.IsAbs() {
		return address.String()
	}
	origin, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return ""
	}
	return origin.ResolveReference(address).String()
}
