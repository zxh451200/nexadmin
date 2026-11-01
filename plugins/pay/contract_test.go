package pay

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"nexadmin/core"
	"nexadmin/core/config"
)

// 契约金标准：docs/contracts/02-openapi-pay-v1.yaml 是插件对外承诺的接口面
// （plugin.yaml 的描述、dto.go 的字段名、前端 api.ts 都对齐它）。
// 此前该文件根本不存在，「契约」只是注释里的引用——这条用例把它变成可执行的闸：
// 生产路由表与契约必须逐条相等，多一条少一条都红。
//
// 用生产构造函数拿路由（&module{}.Register），不是复制一份路径清单：复制出来的清单
// 会因为忘记更新而一直绿，起不到闸的作用。

// rePathParam 把 gin 的 :param 与 *wildcard 归一成契约里的 {param}。
var rePathParam = regexp.MustCompile(`(?::[A-Za-z0-9_]+|\*[A-Za-z0-9_]+)`)

// reOASParam 把契约里的 {param} 归一，与 gin 的 :param 用同一个形状比对。
var reOASParam = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// contractAPIPrefix 是插件在生产引擎上的完整前缀；契约的 servers.url 就是它，
// 所以比对时要把生产路径的前缀剥掉。
const contractAPIPrefix = "/api/v1/plugins/pay"

func contractRouteKey(method, path string) string {
	norm := rePathParam.ReplaceAllString(path, "{}")
	return method + " " + norm
}

// productionRouteKeys 用插件的真实 Register 建一棵路由树并列出 method+path。
func productionRouteKeys(t *testing.T) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 生产形态：管理端与公开端都挂在 /api/v1 下，Register 内部再拼 /plugins/pay。
	api := engine.Group("/api/v1")
	d := &core.Deps{
		Config: &config.Config{},
		Public: api,
	}
	// Register 只装配路由与中间件，不触库；请求期才用 DB，因此这里不建夹具库。
	(&module{}).Register(api, d)
	keys := make([]string, 0, len(engine.Routes()))
	for _, r := range engine.Routes() {
		path := strings.TrimPrefix(r.Path, contractAPIPrefix)
		keys = append(keys, contractRouteKey(r.Method, path))
	}
	sort.Strings(keys)
	return keys
}

// openAPIRouteKeys 解析契约里声明的 path + method 集合。
func openAPIRouteKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(contractPath(t))
	if err != nil {
		t.Fatalf("读取契约文件失败：%v（契约被 plugin.yaml、dto.go、handler.go、api.ts 多处引用，必须存在）", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("契约文件不是合法 YAML：%v", err)
	}
	keys := make([]string, 0, len(doc.Paths))
	for path, ops := range doc.Paths {
		norm := reOASParam.ReplaceAllString(path, "{}")
		for method := range ops {
			keys = append(keys, strings.ToUpper(method)+" "+norm)
		}
	}
	sort.Strings(keys)
	return keys
}

func contractPath(t *testing.T) string {
	t.Helper()
	p := "../../docs/contracts/02-openapi-pay-v1.yaml"
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("契约文件缺失：%s（%v）", p, err)
	}
	return p
}

func TestContractRoutesMatchOpenAPI(t *testing.T) {
	got := productionRouteKeys(t)
	want := openAPIRouteKeys(t)

	gotSet := map[string]bool{}
	for _, k := range got {
		gotSet[k] = true
	}
	wantSet := map[string]bool{}
	for _, k := range want {
		wantSet[k] = true
	}

	var missing, extra []string
	for _, k := range want {
		if !gotSet[k] {
			missing = append(missing, k)
		}
	}
	for _, k := range got {
		if !wantSet[k] {
			extra = append(extra, k)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("路由与契约不一致（契约 %d 条 / 生产 %d 条）\n只在契约里、生产没注册：%v\n已在生产、契约没写：%v",
			len(want), len(got), missing, extra)
	}
	if len(got) == 0 {
		t.Fatal("生产路由表为空——Register 没被真正执行，本用例形同虚设")
	}
}

// TestContractErrorCodesAreLocalized 钉住「错误码即契约」：代码里用到的每个 PAY_* key
// 都要同时出现在契约 ApiError.reason 的枚举与两个语言的字典里，否则调用方拿到的就是
// 没有文案的码（reason 是上线的机器码，见 core/response.Fail）。
func TestContractErrorCodesAreLocalized(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range []string{
		errInvalidAmount, errCurrencyUnsupported, errFxRateMissing, errStateConflict,
		errExpired, errRefundExceeds, errSignInvalid, errChannelUnavailable, errChannelError,
		errNotFound, errTenantMismatch, errIdempotencyConflict, errInternal,
		errCapabilityUnsupported, errNotifyStaleKey, errPayoutSettlementTaken,
	} {
		keys[strings.TrimPrefix(k, "pay.")] = true
	}

	raw, err := os.ReadFile(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas struct {
				ApiError struct {
					Properties struct {
						Reason struct {
							Enum []string `yaml:"enum"`
						} `yaml:"reason"`
					} `yaml:"properties"`
				} `yaml:"ApiError"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	inDoc := map[string]bool{}
	for _, e := range doc.Components.Schemas.ApiError.Properties.Reason.Enum {
		inDoc[e] = true
	}
	for k := range keys {
		if !inDoc[k] {
			t.Errorf("错误码 %s 在代码里会被返回，但契约的 ApiError.reason 枚举里没有", k)
		}
	}
	if len(doc.Components.Schemas.ApiError.Properties.Reason.Enum) == 0 {
		t.Fatal("契约里没有 ApiError.reason 枚举，本用例形同虚设")
	}

	// 字典侧：core/i18n/locales/<lang>.yaml 的 pay 段必须覆盖同一集合。
	// 整份文件是多层结构（不只是 map[string]string），所以只取 pay 段再判键。
	for _, lang := range []string{"zh-CN", "en-US"} {
		p := "../../core/i18n/locales/" + lang + ".yaml"
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		var all map[string]any
		if err := yaml.Unmarshal(body, &all); err != nil {
			t.Fatalf("%s 解析失败：%v", p, err)
		}
		paySec, ok := all["pay"].(map[string]any)
		if !ok {
			t.Fatalf("%s 缺 pay 段", p)
		}
		for k := range keys {
			if _, ok := paySec[k]; !ok {
				t.Errorf("%s 缺少错误码文案 pay.%s", p, k)
			}
		}
	}
}
