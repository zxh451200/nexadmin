// Package i18n 是后端多语言：框架 core/i18n/locales 打底，config/locales 同名 key 覆盖。
// 缺翻译按 当前语言 -> 默认语言 -> key 本身 逐级回退，不报错。
package i18n

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"

	"nexadmin/core/config"
	"nexadmin/pkg/idgen"
)

//go:embed locales/*.yaml
var builtin embed.FS

// Bundle 保存所有语言的扁平 key -> 文案。
type Bundle struct {
	langs []config.Language
	def   string
	msgs  map[string]map[string]string
}

// New 加载内置语言包，再用 overrideDir（可不存在）里的同名文件覆盖。
func New(langs []config.Language, def, overrideDir string) (*Bundle, error) {
	b := &Bundle{langs: langs, def: def, msgs: map[string]map[string]string{}}
	entries, err := builtin.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		data, err := builtin.ReadFile("locales/" + e.Name())
		if err != nil {
			return nil, err
		}
		if err := b.merge(strings.TrimSuffix(e.Name(), ".yaml"), data); err != nil {
			return nil, fmt.Errorf("i18n: builtin %s: %w", e.Name(), err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(overrideDir, "*.yaml"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if err := b.merge(strings.TrimSuffix(filepath.Base(f), ".yaml"), data); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", f, err)
		}
	}
	return b, nil
}

func (b *Bundle) merge(lang string, data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	if b.msgs[lang] == nil {
		b.msgs[lang] = map[string]string{}
	}
	flatten("", raw, b.msgs[lang])
	return nil
}

func flatten(prefix string, in map[string]any, out map[string]string) {
	for k, v := range in {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			flatten(key, t, out)
		default:
			out[key] = fmt.Sprint(t)
		}
	}
}

// T 取文案；有 args 时按 fmt.Sprintf 格式化。
func (b *Bundle) T(lang, key string, args ...any) string {
	msg, ok := b.msgs[lang][key]
	if !ok {
		msg, ok = b.msgs[b.def][key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// Languages 返回配置的语言清单（语言清单唯一来源）。
func (b *Bundle) Languages() []config.Language { return b.langs }

// Negotiate 按 Accept-Language 协商语言，没有匹配则返回默认语言。
func (b *Bundle) Negotiate(header string) string {
	type cand struct {
		tag string
		q   float64
	}
	var cands []cand
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if fields[0] == "" {
			continue
		}
		q := 1.0
		for _, p := range fields[1:] {
			if p = strings.TrimSpace(p); strings.HasPrefix(p, "q=") {
				if v, err := strconv.ParseFloat(p[2:], 64); err == nil {
					q = v
				}
			}
		}
		if q <= 0 { // q=0 表示“不接受”
			continue
		}
		cands = append(cands, cand{strings.ToLower(fields[0]), q})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].q > cands[j].q })
	for _, c := range cands {
		for _, l := range b.langs { // 精确匹配
			if strings.EqualFold(l.Code, c.tag) {
				return l.Code
			}
		}
		base := strings.SplitN(c.tag, "-", 2)[0]
		for _, l := range b.langs { // zh -> zh-CN
			if strings.EqualFold(strings.SplitN(l.Code, "-", 2)[0], base) {
				return l.Code
			}
		}
	}
	return b.def
}

type langKey struct{}

// WithLang 把语言写入 context（由 middleware.I18n 调用）。
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, langKey{}, lang)
}

// Lang 读取 context 里的语言，没有则返回空串。
func Lang(ctx context.Context) string {
	s, _ := ctx.Value(langKey{}).(string)
	return s
}

var def atomic.Pointer[Bundle]

var runtimeMu sync.RWMutex
var runtime = map[string]map[string]string{} // tenant:lang -> key/value

// SetRuntimeOverrides 设置租户运行时文案覆盖；传入 nil 清空该语言覆盖。
func SetRuntimeOverrides(tid idgen.ID, lang string, values map[string]string) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	k := fmt.Sprintf("%d:%s", tid, lang)
	if values == nil {
		delete(runtime, k)
		return
	}
	cp := map[string]string{}
	for key, value := range values {
		cp[key] = value
	}
	runtime[k] = cp
}

// SetDefault 设置全局语言包，T(ctx, key) 使用它。
func SetDefault(b *Bundle) { def.Store(b) }

// T 是业务代码取文案的入口：T(ctx, "user.not_found")。
func T(ctx context.Context, key string, args ...any) string {
	b := def.Load()
	if b == nil {
		return key
	}
	lang := Lang(ctx)
	if lang == "" {
		lang = b.def
	}
	if tenantID := runtimeTenant(ctx); tenantID != "" {
		runtimeMu.RLock()
		value, exists := runtime[tenantID+":"+lang][key]
		runtimeMu.RUnlock()
		if exists {
			if len(args) > 0 {
				return fmt.Sprintf(value, args...)
			}
			return value
		}
	}
	return b.T(lang, key, args...)
}

type tenantContextKey struct{}

// WithRuntimeTenant associates a tenant key without importing core/tenant.
func WithRuntimeTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenant)
}

func runtimeTenant(ctx context.Context) string {
	v, _ := ctx.Value(tenantContextKey{}).(string)
	return v
}

// Default 返回全局语言包（可能为 nil）。
func Default() *Bundle { return def.Load() }
