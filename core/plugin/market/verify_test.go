package market

// 下载完整性校验的测试：httptest 造下载源与校验端点，重点验证
// 「篡改过的包一定装不上」「校验值缺失时按声明的策略走」「失败不留残包」。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// captureTempZip 把下载的临时文件重定向到独立目录，返回该目录；
// 用例结束后目录必须只剩通过校验的包（或空），用来断言失败路径的清理。
func captureTempZip(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := createTempZip
	createTempZip = func() (*os.File, error) { return os.CreateTemp(dir, "nexadmin-market-*.zip") }
	t.Cleanup(func() { createTempZip = old })
	return dir
}

// captureInstallWarn 接管安装的告警输出。
func captureInstallWarn(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	old := installWarn
	installWarn = &buf
	t.Cleanup(func() { installWarn = old })
	return &buf
}

// mustDownload 下载成功后立刻删掉临时包：通过校验的包不该影响后续的“失败不留残包”断言。
func mustDownload(t *testing.T, ctx context.Context, e Entry) string {
	t.Helper()
	p, err := Download(ctx, e)
	if err != nil {
		t.Fatalf("download %s: %v", e.URL, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("downloaded package missing: %v", err)
	}
	os.Remove(p) //nolint:errcheck
	return p
}

func remainingZips(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// zipHandler 按路径回包；sha256sum 格式的侧文件由同一份 zip 现算。
func zipHandler(t *testing.T, zipBytes []byte, sidecar bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mirror/p.zip", "/plugins/cron/download":
			_, _ = w.Write(zipBytes)
		case "/plugins/cron/download.sha256":
			if !sidecar {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprintf(w, "%s  %s\n", hexSum(zipBytes), "cron-1.0.0.zip")
		default:
			http.NotFound(w, r)
		}
	})
}

// 声明了校验值就必须匹配：正确值通过，篡改过的包一律拒绝且不留残包。
func TestDownloadVerifiesDeclaredChecksum(t *testing.T) {
	good := makeZipBytes(t, map[string]string{"plugin.yaml": manifestYAML, "backend/plugin.go": "package demo\n"})
	bad := makeZipBytes(t, map[string]string{"plugin.yaml": manifestYAML, "backend/plugin.go": "package demo\n// injected\n"})

	srv := httptest.NewServer(zipHandler(t, bad, false))
	t.Cleanup(srv.Close)
	dir := captureTempZip(t)
	ctx := context.Background()
	want := hexSum(good)

	// 服务端给的是被换过的包（与目录声明差一个字节）——必须红。
	p, err := Download(ctx, Entry{ID: "demo", URL: srv.URL + "/mirror/p.zip", Checksum: strings.ToUpper(want)})
	if err == nil {
		os.Remove(p) //nolint:errcheck
		t.Fatal("tampered package accepted although the checksum does not match")
	}
	var ie *IntegrityError
	if !errors.As(err, &ie) || ie.Key != keyChecksumMismatch {
		t.Errorf("want mismatch integrity error, got %v", err)
	}
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), hexSum(bad)) {
		t.Errorf("error should quote expected and actual sums: %v", err)
	}
	if got := remainingZips(t, dir); len(got) != 0 {
		t.Errorf("failed verification left downloads behind: %v", got)
	}

	// 正确的校验值必须通过（大小写不敏感）。
	srv2 := httptest.NewServer(zipHandler(t, good, false))
	t.Cleanup(srv2.Close)
	mustDownload(t, ctx, Entry{ID: "demo", URL: srv2.URL + "/mirror/p.zip", Checksum: strings.ToUpper(want)})

	// 目录里的校验值本身不合法：拒绝，不能拿它去“比较通过”。
	if _, err := Download(ctx, Entry{ID: "demo", URL: srv2.URL + "/mirror/p.zip", Checksum: "deadbeef"}); err == nil {
		t.Error("malformed checksum accepted")
	} else {
		var ie *IntegrityError
		if !errors.As(err, &ie) || ie.Key != keyChecksumInvalid {
			t.Errorf("want invalid checksum error, got %v", err)
		}
	}
}

// 官网这一路：期望值直接取现成的 download.sha256 端点，不需要人工填字段。
func TestDownloadWebsiteSidecarChecksum(t *testing.T) {
	good := makeZipBytes(t, map[string]string{"plugin.yaml": manifestYAML, "backend/plugin.go": "package demo\n"})
	srv := httptest.NewServer(zipHandler(t, good, true))
	t.Cleanup(srv.Close)
	dir := captureTempZip(t)
	ctx := context.Background()

	// 没有 checksum 字段，但下载地址是官网端点：自动取侧文件并通过校验。
	mustDownload(t, ctx, Entry{ID: "cron", URL: srv.URL + "/plugins/cron/download"})

	// 侧文件说的是另一个包（镜像被换过 / 缓存被投毒）：必须红。
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download.sha256") {
			_, _ = fmt.Fprintf(w, "%s  cron-1.0.0.zip\n", hexSum([]byte("something else")))
			return
		}
		_, _ = w.Write(good)
	}))
	t.Cleanup(attacker.Close)
	if _, err := Download(ctx, Entry{ID: "cron", URL: attacker.URL + "/plugins/cron/download"}); err == nil {
		t.Fatal("download accepted against a mismatching checksum endpoint")
	} else {
		var ie *IntegrityError
		if !errors.As(err, &ie) || ie.Key != keyChecksumMismatch {
			t.Errorf("want mismatch integrity error, got %v", err)
		}
	}

	// 显式声明 checksum_url 时，取不到就是拒绝（不是“当作没填”）。
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(good)
	}))
	t.Cleanup(failing.Close)
	if _, err := Download(ctx, Entry{ID: "cron", URL: failing.URL + "/mirror/p.zip", ChecksumURL: failing.URL + "/mirror/p.sha256"}); err == nil {
		t.Fatal("declared checksum_url unavailable but install accepted")
	} else {
		var ie *IntegrityError
		if !errors.As(err, &ie) || ie.Key != keyChecksumUnavailable {
			t.Errorf("want unavailable integrity error, got %v", err)
		}
	}
	if got := remainingZips(t, dir); len(got) != 0 {
		t.Errorf("refused downloads left behind: %v", got)
	}
}

// 无校验值时的默认策略：告警放行（不静默）；严格开关打开则拒绝。
func TestDownloadUnverifiedPolicy(t *testing.T) {
	good := makeZipBytes(t, map[string]string{"plugin.yaml": manifestYAML, "backend/plugin.go": "package demo\n"})
	srv := httptest.NewServer(zipHandler(t, good, false))
	t.Cleanup(srv.Close)
	dir := captureTempZip(t)
	ctx := context.Background()
	entry := Entry{ID: "demo", URL: srv.URL + "/mirror/p.zip"}

	// 默认：装得上，但必须在告警输出里说清楚“未经完整性校验”。
	warn := captureInstallWarn(t)
	mustDownload(t, ctx, entry)
	if !strings.Contains(warn.String(), "未经完整性校验") {
		t.Errorf("missing unverified warning, got %q", warn.String())
	}

	// 官网端点但侧文件 404：同样退回默认策略（不因为猜不到侧文件就硬失败）。
	noSidecar := httptest.NewServer(zipHandler(t, good, false))
	t.Cleanup(noSidecar.Close)
	warn2 := captureInstallWarn(t)
	mustDownload(t, ctx, Entry{ID: "cron", URL: noSidecar.URL + "/plugins/cron/download"})
	if !strings.Contains(warn2.String(), "未经完整性校验") {
		t.Errorf("missing sidecar should warn, got %q", warn2.String())
	}

	// 严格模式：两条路都拒绝，且拒绝后磁盘上不留残包。
	t.Setenv(EnvRequireChecksum, "1")
	for _, u := range []string{srv.URL + "/mirror/p.zip", noSidecar.URL + "/plugins/cron/download"} {
		if _, err := Download(ctx, Entry{ID: "demo", URL: u}); err == nil {
			t.Errorf("strict mode accepted %s", u)
			continue
		} else {
			var ie *IntegrityError
			if !errors.As(err, &ie) || ie.Key != keyChecksumMissing {
				t.Errorf("want checksum_missing, got %v", err)
			}
		}
	}
	if got := remainingZips(t, dir); len(got) != 0 {
		t.Errorf("strict refusals left downloads behind: %v", got)
	}

	// GitHub zipball 没有校验值可取：严格模式同样拒绝（消费侧一视同仁）。
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(good)
	}))
	t.Cleanup(gh.Close)
	old := GitHubBaseURL
	GitHubBaseURL = gh.URL
	t.Cleanup(func() { GitHubBaseURL = old })
	if _, err := Download(ctx, Entry{ID: "demo", Repo: "acme/plugins"}); err == nil {
		t.Error("strict mode accepted an unverifiable zipball")
	}
}

func TestChecksumParsingAndSidecarURL(t *testing.T) {
	sum := hexSum([]byte("x"))
	for _, text := range []string{
		sum,
		sum + "  demo.zip\n",
		strings.ToUpper(sum) + " *demo.zip\r\n",
		"# comment\n" + sum + "  demo.zip\n",
		"SHA256 (demo.zip) = " + sum + "\n",
	} {
		if got, err := ParseChecksumFile(text); err != nil || got != sum {
			t.Errorf("ParseChecksumFile(%q) = %q, %v", text, got, err)
		}
	}
	for _, text := range []string{"", "md5  d41d8cd98f00b204e9800998ecf8427e  x", "not-a-sum", sum[:63] + "z  x"} {
		if _, err := ParseChecksumFile(text); err == nil {
			t.Errorf("ParseChecksumFile(%q) should fail", text)
		}
	}

	if got := sidecarChecksumURL("https://site.example/plugins/cron/download"); got != "https://site.example/plugins/cron/download.sha256" {
		t.Errorf("sidecar = %q", got)
	}
	if got := sidecarChecksumURL("https://site.example/plugins/cron/download?version=0.9.0"); got != "https://site.example/plugins/cron/download.sha256?version=0.9.0" {
		t.Errorf("sidecar with version = %q", got)
	}
	for _, u := range []string{"https://mirror/os/demo.zip", "https://site.example/plugins/cron/", "://bad"} {
		if got := sidecarChecksumURL(u); got != "" {
			t.Errorf("sidecar for %q = %q, want empty", u, got)
		}
	}

	for _, v := range []string{"1", "true", "YES", " on "} {
		t.Setenv(EnvRequireChecksum, v)
		if !RequireChecksum() {
			t.Errorf("RequireChecksum with %q = false", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off"} {
		t.Setenv(EnvRequireChecksum, v)
		if RequireChecksum() {
			t.Errorf("RequireChecksum with %q = true", v)
		}
	}
}

// 校验失败要落进 zip 之前：verifyPackage 直接对着临时文件验，通过才交给解压。
func TestVerifyPackageBeforeExtract(t *testing.T) {
	good := makeZipBytes(t, map[string]string{"plugin.yaml": manifestYAML, "backend/plugin.go": "package demo\n"})
	path := filepath.Join(t.TempDir(), "p.zip")
	if err := os.WriteFile(path, good, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	if err := verifyPackage(context.Background(), path, "https://mirror/os/p.zip", Entry{ID: "demo", Checksum: hexSum(good)}); err != nil {
		t.Fatalf("matching checksum: %v", err)
	}
	if err := verifyPackage(context.Background(), path, "https://mirror/os/p.zip", Entry{ID: "demo", Checksum: hexSum([]byte("other"))}); err == nil {
		t.Fatal("mismatching checksum accepted")
	}
	// 校验不过时调用方不会走到解压：目标目录保持不存在。
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("extraction happened for an unverified package: %v", err)
	}
	if err := ExtractZip(path, dest); err != nil {
		t.Fatalf("extract after verification: %v", err)
	}
}

// 目录字段：YAML 与 JSON 都要能解析，非法值在建目录时就拒绝。
func TestCatalogChecksumFields(t *testing.T) {
	sum := hexSum([]byte("x"))
	entries, err := catalogWith(t, fmt.Sprintf("    checksum: %s\n    checksum_url: https://mirror/cron.sha256\n", sum))
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Checksum != sum || entries[0].ChecksumURL != "https://mirror/cron.sha256" {
		t.Errorf("checksum fields = %q / %q", entries[0].Checksum, entries[0].ChecksumURL)
	}
	if _, err := catalogWith(t, "    checksum: zzz\n"); err == nil {
		t.Error("malformed catalog checksum accepted")
	}
	if _, err := catalogWith(t, "    checksum_url: ftp://mirror/x\n"); err == nil {
		t.Error("non http(s) checksum_url accepted")
	}
	if _, err := catalogWith(t, "    versions:\n      - version: 0.9.0\n        ref: v0.9.0\n        checksum: short\n"); err == nil {
		t.Error("malformed per-version checksum accepted")
	}

	// JSON 侧（导出与外部工具消费同一套字段名）。
	var e Entry
	raw := fmt.Sprintf(`{"id":"cron","checksum":"%s","checksumUrl":"https://mirror/cron.sha256"}`, sum)
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	if e.Checksum != sum || e.ChecksumURL != "https://mirror/cron.sha256" {
		t.Errorf("json fields = %+v", e)
	}
}

// 旧版本有旧版本的字节：版本级 checksum 覆盖条目级。
func TestEntryForVersionCarriesOwnChecksum(t *testing.T) {
	old := hexSum([]byte("old"))
	e := Entry{ID: "cron", Version: "1.0.0", Ref: "v1.0.0", Checksum: hexSum([]byte("new")),
		ChecksumURL: "https://mirror/new.sha256",
		Versions:    []Version{{Version: "0.9.0", Ref: "v0.9.0", Checksum: old}}}
	got, ok := EntryForVersion(e, "0.9.0")
	if !ok {
		t.Fatal("version not resolvable")
	}
	if got.Checksum != old {
		t.Errorf("version checksum = %q, want %q", got.Checksum, old)
	}
	if got.ChecksumURL != "" {
		t.Errorf("stale entry checksum_url kept: %q", got.ChecksumURL)
	}
	if cur, _ := EntryForVersion(e, "1.0.0"); cur.Checksum != e.Checksum {
		t.Error("current version must keep the entry checksum")
	}
}

// 上屏文案的两份语言包必须同 key、同占位符个数——校验失败的信息不能只有一种语言。
func TestChecksumLocaleKeysParity(t *testing.T) {
	keys := []string{keyChecksumMismatch, keyChecksumInvalid, keyChecksumUnavailable, keyChecksumMissing, keyChecksumUnverified}
	verbRe := regexp.MustCompile(`%[a-z]`)
	load := func(lang string) (map[string]string, int) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "i18n", "locales", lang+".yaml"))
		if err != nil {
			t.Fatalf("read %s: %v", lang, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", lang, err)
		}
		flat := map[string]string{}
		flattenLocale("", doc, flat)
		return flat, strings.Count(string(raw), "\n")
	}
	zh, zhLines := load("zh-CN")
	en, enLines := load("en-US")
	if zhLines != enLines {
		t.Errorf("locale files must stay line-aligned: zh-CN %d lines, en-US %d lines", zhLines, enLines)
	}
	for _, k := range keys {
		a, b := zh[k], en[k]
		if a == "" {
			t.Errorf("zh-CN.yaml missing %s", k)
		}
		if b == "" {
			t.Errorf("en-US.yaml missing %s", k)
		}
		if za, eb := verbRe.FindAllString(a, -1), verbRe.FindAllString(b, -1); len(za) != len(eb) {
			t.Errorf("%s: zh-CN has %d placeholders, en-US has %d", k, len(za), len(eb))
		}
	}
	// 兜底文案（语言包未加载时渲染）与 zh-CN 的占位符个数也要一致，否则日志里会漏参数。
	for _, k := range keys {
		if fb := verbRe.FindAllString(fallbackTemplates[k], -1); len(fb) != len(verbRe.FindAllString(zh[k], -1)) {
			t.Errorf("%s: fallback placeholder count differs from zh-CN", k)
		}
	}
}

func flattenLocale(prefix string, in map[string]any, out map[string]string) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			flattenLocale(key, child, out)
			continue
		}
		out[key] = fmt.Sprint(v)
	}
}
