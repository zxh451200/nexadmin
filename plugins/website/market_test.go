package website

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/plugin"
	"nexadmin/core/plugin/market"
)

func testEntries() []market.Entry {
	return []market.Entry{
		{ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro, Category: "devops",
			Description: "5 段 cron 调度、立即执行、失败重试。", Author: "NexAdmin", Price: "¥299", Repo: "acme/plugins",
			Screenshots: []string{"/website-assets/og.png"}, ChangelogURL: "https://example.com/changelog",
			Versions: []market.Version{{Version: "0.9.0", Date: "2026-01-02", Ref: "v0.9.0", Notes: "首个版本"}}},
		{ID: "audit", Name: "数据变更审计", Version: "1.2.0", Edition: plugin.Pro, Category: "security",
			Description: "字段级前后对比与脱敏。", Author: "NexAdmin", Price: "待定", Repo: "acme/plugins"},
		{ID: "demo", Name: "示例插件", Version: "0.1.0", Edition: plugin.Free, Category: "devops",
			Description: "免费示例。", Author: "社区"},
	}
}

func TestFilterMarket(t *testing.T) {
	all := testEntries()
	cases := []struct {
		q, cat, ed string
		want       []string
	}{
		{"", "", "", []string{"cron", "audit", "demo"}},
		{"审计", "", "", []string{"audit"}},
		{"CRON", "", "", []string{"cron"}},
		{"研发与运维", "", "", []string{"cron", "demo"}}, // 分类展示名也参与搜索
		{"", "security", "", []string{"audit"}},
		{"", "", "free", []string{"demo"}},
		{"", "devops", "pro", []string{"cron"}},
		{"没有这个", "", "", nil},
	}
	for _, c := range cases {
		got := filterMarket(all, c.q, c.cat, c.ed, "zh-CN")
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if strings.Join(ids, ",") != strings.Join(c.want, ",") {
			t.Errorf("filter(%q,%q,%q) = %v, want %v", c.q, c.cat, c.ed, ids, c.want)
		}
	}
}

func TestFeaturedEntries(t *testing.T) {
	entries := []market.Entry{
		{ID: "a"}, {ID: "b", Screenshots: []string{"/x.jpg"}}, {ID: "c"},
		{ID: "d", Screenshots: []string{"/y.jpg"}},
	}
	got := featuredEntries(entries, 3)
	ids := make([]string, 0, 3)
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	if strings.Join(ids, ",") != "b,d,a" {
		t.Errorf("featured = %v, want [b d a]", ids)
	}
}

func TestMarketChips(t *testing.T) {
	cats, eds := marketChips(testEntries(), "cron", "devops", "pro", "zh-CN")
	if len(cats) != 3 { // 全部 + security + devops（目录顺序）
		t.Fatalf("categories = %d, want 3", len(cats))
	}
	if cats[0].Active || cats[0].Label != "全部" {
		t.Errorf("first chip should be inactive 全部: %+v", cats[0])
	}
	var devops *marketChip
	for i := range cats {
		if cats[i].Label == "研发与运维" {
			devops = &cats[i]
		}
	}
	if devops == nil || !devops.Active {
		t.Fatalf("devops chip missing or inactive: %+v", cats)
	}
	if !strings.Contains(devops.Href, "q=cron") || !strings.Contains(devops.Href, "edition=pro") {
		t.Errorf("category href should preserve filters: %q", devops.Href)
	}
	if len(eds) != 3 || !eds[2].Active || eds[2].Label != "专业" {
		t.Errorf("edition chips wrong: %+v", eds)
	}
	// 选中项再点一次 = 取消该维筛选。
	if strings.Contains(cats[0].Href, "category=") {
		t.Errorf("全部 href should drop category: %q", cats[0].Href)
	}
}

func TestMarketPackageName(t *testing.T) {
	if got := marketPackageName(market.Entry{ID: "cron", Version: "1.0.0"}); got != "cron-1.0.0.zip" {
		t.Errorf("name = %q", got)
	}
	// 版本号进文件名前必须净化。
	if got := marketPackageName(market.Entry{ID: "cron", Version: "../../x"}); got != "cron-latest.zip" {
		t.Errorf("unsafe version = %q", got)
	}
	if got := marketInstallCommand(market.Entry{ID: "cron", Version: "1.0.0"}); got != "nexadmin plugin install cron-1.0.0.zip" {
		t.Errorf("install command = %q", got)
	}
}

func TestMarketRoutePatterns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	noop := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	r.GET("/plugins", noop)
	r.GET("/plugins/index.json", noop)
	r.GET("/plugins/:id", noop)
	r.GET("/plugins/:id/download", noop)
	r.GET("/plugins/:id/download.sha256", noop)

	for path, want := range map[string]int{
		"/plugins":                      http.StatusNoContent,
		"/plugins/index.json":           http.StatusNoContent,
		"/plugins/cron":                 http.StatusNoContent,
		"/plugins/cron/download":        http.StatusNoContent,
		"/plugins/cron/download.sha256": http.StatusNoContent,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != want {
			t.Errorf("%s => %d, want %d", path, w.Code, want)
		}
	}
}

func marketView() *siteView {
	v := testView()
	v.MarketAvailable = true
	v.PluginCount = 3
	v.MarketFiltered = 3
	v.Plugins = marketCards(testEntries(), nil, "zh-CN")
	v.MarketCategories, v.MarketEditions = marketChips(testEntries(), "", "", "", "zh-CN")
	v.Plugin = &v.Plugins[0]
	v.Plugin.Versions = marketVersionViews(testEntries()[0], true)
	v.Related = v.Plugins[1:]
	v.MyScore = 4
	v.Signed = true
	v.SignatureURL = "/plugins/cron/download.sig"
	v.PublicKeyURL = "/plugins/signing-key.pub"
	v.DownloadURL = v.Plugins[0].DownloadURL
	v.DownloadFilename = "cron-1.0.0.zip"
	v.InstallCommand = "nexadmin plugin install cron-1.0.0.zip"
	v.MarketSourceURL = "https://github.com/acme/plugins"
	v.AdminInstallURL = "/login?redirect=" + url.QueryEscape("/system/plugin?tab=market&plugin=cron")
	v.PluginLD = pluginJSONLD(testEntries()[0], v.BaseURL, "/plugins/cron")
	return v
}

// 下载缓存：同一插件只拉一次源站，第二次直接命中磁盘；产物能正常解压。
func TestPackageCacheDownloadsOnce(t *testing.T) {
	var hits int
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"repo/cron/plugin.yaml":       "id: cron\nname: 定时任务\nversion: 1.0.0\nedition: pro\n",
		"repo/cron/backend/plugin.go": "package cron\n",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	old := market.GitHubBaseURL
	market.GitHubBaseURL = srv.URL
	defer func() { market.GitHubBaseURL = old }()

	e := market.Entry{ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro,
		Repo: "acme/plugins", Subdir: "cron"}
	cache := newPackageCache()
	cache.dir = t.TempDir()
	ctx := context.Background()
	p1, err := cache.get(ctx, e)
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	p2, err := cache.get(ctx, e)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if p1 != p2 {
		t.Errorf("cache path changed: %q vs %q", p1, p2)
	}
	if hits != 1 {
		t.Errorf("upstream hits = %d, want 1", hits)
	}
	zr, err := zip.OpenReader(p1)
	if err != nil {
		t.Fatalf("cached package unreadable: %v", err)
	}
	defer zr.Close() //nolint:errcheck
	if len(zr.File) != 2 {
		t.Errorf("cached package entries = %d, want 2", len(zr.File))
	}
}

// 失败后 1 分钟内不重复拉源：排队/重试请求不会把一次抖动放大成 N 次 GitHub 拉取。
func TestPackageCacheFailureNotAmplified(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := market.GitHubBaseURL
	market.GitHubBaseURL = srv.URL
	defer func() { market.GitHubBaseURL = old }()

	cache := newPackageCache()
	cache.dir = t.TempDir()
	e := market.Entry{ID: "cron", Name: "定时任务", Version: "1.0.0", Edition: plugin.Pro,
		Repo: "acme/plugins", Subdir: "cron"}
	for i := 0; i < 3; i++ {
		if _, err := cache.get(context.Background(), e); err == nil {
			t.Fatalf("attempt %d: want error", i)
		}
	}
	if hits != 1 {
		t.Errorf("upstream hits = %d, want 1 (negative cache)", hits)
	}
}

// 机器可读目录：字段与页面同源，含下载、校验和与安装命令的绝对地址。
func TestMarketIndexJSON(t *testing.T) {
	h := &siteHandler{catalog: func() []market.Entry { return testEntries() }, marketOn: func() (bool, error) { return true, nil }}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/plugins/index.json", h.marketIndexJSON)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://example.com/plugins/index.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var items []marketIndexItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	if items[0].ChecksumURL != "http://example.com/plugins/cron/download.sha256" {
		t.Errorf("checksumUrl = %q", items[0].ChecksumURL)
	}
	if items[0].InstallCommand != "nexadmin plugin install cron-1.0.0.zip" {
		t.Errorf("installCommand = %q", items[0].InstallCommand)
	}
}

// 下载接口的 HTTP 契约：200 + zip + 附件名；打包来源用本地假 GitHub。
func TestMarketDownloadHandler(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"repo/cron/plugin.yaml":       "id: cron\nname: 定时任务\nversion: 0.0.0-dltest\nedition: pro\n",
		"repo/cron/backend/plugin.go": "package cron\n",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	old := market.GitHubBaseURL
	market.GitHubBaseURL = srv.URL
	defer func() { market.GitHubBaseURL = old }()

	e := market.Entry{ID: "cron", Name: "定时任务", Version: "0.0.0-dltest", Edition: plugin.Pro,
		Repo: "acme/plugins", Subdir: "cron"}
	cache := newPackageCache()
	cache.dir = t.TempDir()
	h := &siteHandler{pkg: cache, catalog: func() []market.Entry { return []market.Entry{e} }, marketOn: func() (bool, error) { return true, nil }}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/plugins/:id/download", h.marketDownload)
	r.GET("/plugins/:id/download.sha256", h.marketChecksum)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/plugins/cron/download", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("content-type = %q, want application/zip", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, marketPackageName(e)) {
		t.Errorf("content-disposition = %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("body is not a zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["plugin.yaml"] || !names["backend/plugin.go"] {
		t.Errorf("unexpected package entries: %v", names)
	}

	sum := sha256.Sum256(w.Body.Bytes())
	wantSum := hex.EncodeToString(sum[:])
	if got := w.Header().Get("X-Checksum-SHA256"); got != wantSum {
		t.Errorf("checksum header = %q, want %q", got, wantSum)
	}

	// 校验文件与下载产物必须一一对应（sha256sum 格式）。
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/plugins/cron/download.sha256", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("checksum status = %d, body = %s", w2.Code, w2.Body.String())
	}
	wantLine := wantSum + "  " + marketPackageName(e) + "\n"
	if w2.Body.String() != wantLine {
		t.Errorf("checksum body = %q, want %q", w2.Body.String(), wantLine)
	}
}

// 结构化数据必须是合法 JSON，且插件名里的 </script> 不能打断脚本块。
func TestPluginJSONLD(t *testing.T) {
	e := market.Entry{ID: "cron", Name: `</script><script>alert(1)</script>`, Description: "desc",
		Version: "1.0.0", Category: "devops", Edition: plugin.Pro, Author: "NexAdmin"}
	raw := string(pluginJSONLD(e, "https://example.com", "/plugins/cron"))
	if strings.Contains(raw, "</script>") {
		t.Fatalf("script terminator not escaped: %s", raw)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("invalid JSON-LD: %v", err)
	}
	if m["@type"] != "SoftwareApplication" || m["name"] != e.Name {
		t.Errorf("unexpected JSON-LD: %v", m)
	}
	if m["downloadUrl"] != "https://example.com/plugins/cron/download" {
		t.Errorf("downloadUrl = %v", m["downloadUrl"])
	}
}

func TestRenderMarketPages(t *testing.T) {
	v := marketView()
	render := func(name string) string {
		t.Helper()
		tpl, ok := pageTmpl[name]
		if !ok {
			t.Fatalf("template %q missing", name)
		}
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "layout", v); err != nil {
			t.Fatalf("render %q: %v", name, err)
		}
		return b.String()
	}

	list := render("plugins")
	for _, want := range []string{
		"插件市场", "定时任务", "/plugins/cron", "专业", "免费",
		`action="/plugins"`, "market-toolbar",
	} {
		if !strings.Contains(list, want) {
			t.Errorf("plugins page missing %q", want)
		}
	}

	detail := render("plugin")
	for _, want := range []string{
		"在后台一键安装", "下载源码包", "nexadmin plugin install cron-1.0.0.zip",
		"/plugins/cron/download", "/plugins/cron/download.sha256",
		"redirect=%2Fsystem%2Fplugin%3Ftab%3Dmarket%26plugin%3Dcron",
		"导入授权", "同类插件", "application/ld+json", "版本历史", "评分", "plugin-shots", "Ed25519 签名", "v0.9.0",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("plugin page missing %q", want)
		}
	}

	v.MarketError = "插件源暂时不可用。"
	errPage := render("error")
	if !strings.Contains(errPage, "插件包暂时取不到") || !strings.Contains(errPage, "插件源暂时不可用。") {
		t.Error("error page missing copy")
	}
}
