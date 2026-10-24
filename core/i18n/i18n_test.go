package i18n

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

var testLangs = []config.Language{
	{Code: "zh-CN", Name: "简体中文", Default: true},
	{Code: "en-US", Name: "English"},
}

func newBundle() *Bundle {
	return &Bundle{langs: testLangs, def: "zh-CN", msgs: map[string]map[string]string{
		"zh-CN": {"hello": "你好", "only_zh": "仅中文", "greet": "你好，%s（%d）"},
		"en-US": {"hello": "Hello", "greet": "Hi, %s (%d)"},
	}}
}

func TestNegotiate(t *testing.T) {
	b := newBundle()
	cases := []struct{ name, header, want string }{
		{"空头回落默认", "", "zh-CN"},
		{"精确匹配", "en-US", "en-US"},
		{"大小写不敏感", "EN-us", "en-US"},
		{"仅主语言 zh -> zh-CN", "zh", "zh-CN"},
		{"en-GB 回落到同主语言 en-US", "en-GB", "en-US"},
		{"q 值高者优先", "zh-CN;q=0.5, en-US;q=0.9", "en-US"},
		{"q 相同按出现顺序", "en-US, zh-CN", "en-US"},
		{"第一个不支持则取下一个", "fr-FR, en;q=0.8", "en-US"},
		{"全部不支持回落默认", "fr-FR, de;q=0.8", "zh-CN"},
		{"通配符回落默认", "*", "zh-CN"},
		{"垃圾 q 值按 1 处理", "en-US;q=abc, zh-CN;q=0.5", "en-US"},
		{"空段与空白被忽略", " , ,  en-US ;q=1 ", "en-US"},
		{"q=0 表示不接受", "en-US;q=0", "zh-CN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.Negotiate(tc.header); got != tc.want {
				t.Errorf("Negotiate(%q)=%q 期望 %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestBundleTFallbackChain(t *testing.T) {
	b := newBundle()
	if got := b.T("en-US", "hello"); got != "Hello" {
		t.Errorf("当前语言有译文应直接用: %q", got)
	}
	if got := b.T("en-US", "only_zh"); got != "仅中文" {
		t.Errorf("缺译文应回落默认语言: %q", got)
	}
	if got := b.T("fr-FR", "hello"); got != "你好" {
		t.Errorf("未知语言应回落默认语言: %q", got)
	}
	if got := b.T("en-US", "no.such.key"); got != "no.such.key" {
		t.Errorf("都没有应返回 key 本身: %q", got)
	}
	if got := b.T("en-US", "no.such.key", "x"); got != "no.such.key" {
		t.Errorf("缺 key 带参数也只返回 key，不能格式化 key: %q", got)
	}
}

func TestBundleTPlaceholders(t *testing.T) {
	b := newBundle()
	if got := b.T("en-US", "greet", "Ann", 3); got != "Hi, Ann (3)" {
		t.Errorf("占位符格式化: %q", got)
	}
	if got := b.T("zh-CN", "greet", "小明", 7); got != "你好，小明（7）" {
		t.Errorf("占位符格式化: %q", got)
	}
	if got := b.T("en-US", "hello"); got != "Hello" {
		t.Errorf("无参数时原样返回: %q", got)
	}
}

func TestFlattenNested(t *testing.T) {
	out := map[string]string{}
	flatten("", map[string]any{
		"a":   map[string]any{"b": map[string]any{"c": "deep"}, "n": 5},
		"top": true,
	}, out)
	want := map[string]string{"a.b.c": "deep", "a.n": "5", "top": "true"}
	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s=%q 期望 %q", k, out[k], v)
		}
	}
	if len(out) != len(want) {
		t.Errorf("多出 key: %v", out)
	}
}

func TestNewOverrideDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("en-US.yaml", "error:\n  internal: Custom boom\nbrand:\n  name: Acme\n")
	b, err := New(testLangs, "zh-CN", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.T("en-US", "error.internal"); got != "Custom boom" {
		t.Errorf("config/locales 同名 key 应覆盖内置: %q", got)
	}
	if got := b.T("en-US", "brand.name"); got != "Acme" {
		t.Errorf("覆盖目录可新增 key: %q", got)
	}
	if got := b.T("en-US", "error.bad_request"); got == "error.bad_request" || got == "" {
		t.Errorf("未被覆盖的内置 key 应保留: %q", got)
	}
	if got := b.T("zh-CN", "error.internal"); got == "Custom boom" {
		t.Error("覆盖只作用于对应语言")
	}
	if len(b.Languages()) != 2 {
		t.Error("Languages 应返回配置的语言清单")
	}
}

func TestNewMissingOverrideDirIsFine(t *testing.T) {
	if _, err := New(testLangs, "zh-CN", filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("覆盖目录不存在不应报错: %v", err)
	}
}

func TestNewInvalidOverrideYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en-US.yaml"), []byte("a: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(testLangs, "zh-CN", dir); err == nil {
		t.Fatal("覆盖文件语法错误应返回错误，而不是静默忽略")
	}
}

var verbRe = regexp.MustCompile(`%[-+# 0]*\d*(?:\.\d+)?[a-zA-Z]`)

// 内置中英文语言包必须 key 对齐、占位符个数一致，否则切换语言会缺文案或 Sprintf 出 %!d(MISSING)。
func TestBuiltinLocalesInSync(t *testing.T) {
	b, err := New(testLangs, "zh-CN", filepath.Join(t.TempDir(), "none"))
	if err != nil {
		t.Fatal(err)
	}
	zh, en := b.msgs["zh-CN"], b.msgs["en-US"]
	if len(zh) == 0 || len(en) == 0 {
		t.Fatal("内置语言包为空")
	}
	var keys []string
	for k := range zh {
		keys = append(keys, k)
	}
	for k := range en {
		if _, ok := zh[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		z, zok := zh[k]
		e, eok := en[k]
		if !zok || !eok {
			t.Errorf("key %q 只存在于一种语言 (zh=%v en=%v)", k, zok, eok)
			continue
		}
		if zn, en := len(verbRe.FindAllString(z, -1)), len(verbRe.FindAllString(e, -1)); zn != en {
			t.Errorf("key %q 占位符个数不一致: zh=%d en=%d", k, zn, en)
		}
	}
}

// 以下测试会改全局语言包，用 cleanup 还原。
func useGlobal(t *testing.T, b *Bundle) {
	t.Helper()
	prev := Default()
	SetDefault(b)
	t.Cleanup(func() { SetDefault(prev) })
}

func TestGlobalT(t *testing.T) {
	ctx := context.Background()
	t.Run("未初始化返回 key", func(t *testing.T) {
		useGlobal(t, nil)
		if got := T(ctx, "hello"); got != "hello" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("context 无语言用默认语言", func(t *testing.T) {
		useGlobal(t, newBundle())
		if got := T(ctx, "hello"); got != "你好" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("context 语言生效且带参数", func(t *testing.T) {
		useGlobal(t, newBundle())
		if got := T(WithLang(ctx, "en-US"), "greet", "Bob", 2); got != "Hi, Bob (2)" {
			t.Errorf("got %q", got)
		}
		if Lang(ctx) != "" || Lang(WithLang(ctx, "en-US")) != "en-US" {
			t.Error("Lang 读写不对")
		}
	})
}

func TestRuntimeOverridesAreTenantScoped(t *testing.T) {
	useGlobal(t, newBundle())
	const tenantA, tenantB = idgen.ID(9101), idgen.ID(9102)
	SetRuntimeOverrides(tenantA, "en-US", map[string]string{"hello": "Yo %s"})
	t.Cleanup(func() {
		SetRuntimeOverrides(tenantA, "en-US", nil)
		SetRuntimeOverrides(tenantB, "en-US", nil)
	})
	base := WithLang(context.Background(), "en-US")
	ctxA := WithRuntimeTenant(base, "9101")
	ctxB := WithRuntimeTenant(base, "9102")

	if got := T(ctxA, "hello", "Z"); got != "Yo Z" {
		t.Errorf("租户 A 的覆盖应生效并支持占位符: %q", got)
	}
	if got := T(ctxB, "hello"); got != "Hello" {
		t.Errorf("租户 B 不能读到租户 A 的覆盖: %q", got)
	}
	if got := T(base, "hello"); got != "Hello" {
		t.Errorf("无租户 context 不应使用任何租户覆盖: %q", got)
	}
	if got := T(WithRuntimeTenant(WithLang(context.Background(), "zh-CN"), "9101"), "hello"); got != "你好" {
		t.Errorf("覆盖按语言区分: %q", got)
	}
	if got := T(ctxA, "only_zh"); got != "仅中文" {
		t.Errorf("覆盖里没有的 key 应回落到语言包: %q", got)
	}

	// 入参 map 之后被调用方改动，不能影响已登记的覆盖
	vals := map[string]string{"hello": "Orig"}
	SetRuntimeOverrides(tenantB, "en-US", vals)
	vals["hello"] = "Mutated"
	if got := T(ctxB, "hello"); got != "Orig" {
		t.Errorf("覆盖应是拷贝: %q", got)
	}
	SetRuntimeOverrides(tenantB, "en-US", nil)
	if got := T(ctxB, "hello"); got != "Hello" {
		t.Errorf("传 nil 应清空覆盖: %q", got)
	}
}
