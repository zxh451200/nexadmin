package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	coreai "nexadmin/core/ai"
	"nexadmin/core/plugin/market"

	"gopkg.in/yaml.v3"
)

const i18nUsage = `用法:
  nexadmin i18n add <locale> [--name <显示名>] [--dir ltr|rtl] [--translate]
  nexadmin i18n check [--all]`

const untranslatedKey = "_untranslated"

var localeRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// 常见语言的显示名；其他语言用 --name，缺省显示 locale 代码。
var knownLangs = map[string]string{
	"fr-FR": "Français", "de-DE": "Deutsch", "es-ES": "Español", "ja-JP": "日本語", "ko-KR": "한국어",
	"ru-RU": "Русский", "pt-BR": "Português (Brasil)", "it-IT": "Italiano", "ar-SA": "العربية", "he-IL": "עברית",
	"zh-TW": "繁體中文", "th-TH": "ไทย", "vi-VN": "Tiếng Việt", "tr-TR": "Türkçe", "id-ID": "Bahasa Indonesia",
}

var rtlRe = regexp.MustCompile(`^(ar|he|fa|ur)(-|$)`)

func cmdI18n(args []string, out io.Writer) error {
	if len(args) < 1 {
		return errors.New(i18nUsage)
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	switch args[0] {
	case "add":
		return cmdI18nAdd(root, args[1:], out)
	case "check":
		return cmdI18nCheck(root, args[1:], out)
	}
	return errors.New(i18nUsage)
}

// ---- config/config.yaml 的 languages 行（纯函数） ----

func yamlScalar(s string) string {
	if strings.ContainsAny(s, ",{}:#\"'[]&*!|>%@`") || strings.TrimSpace(s) != s {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}

// languagesBlock 返回 src 中 languages: 块的 [start, end) 行区间；没有返回 -1。
func languagesBlock(lines []string) (int, int) {
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "languages:") {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := start + 1
	for end < len(lines) {
		l := lines[end]
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "#") {
			break
		}
		end++
	}
	return start, end
}

// addLanguageLine 在 config.yaml 文本里加一行语言配置（幂等）。没有 languages: 块时，
// 以 defaults（config/default.yaml 的文本）里的语言列表为底整块写出，避免覆盖掉默认语言。
func addLanguageLine(cfg, defaults, code, name, dir string) (string, bool, error) {
	row := fmt.Sprintf("  - { code: %s, name: %s, dir: %s }", code, yamlScalar(name), dir)
	lines := strings.Split(cfg, "\n")
	s, e := languagesBlock(lines)
	if s >= 0 {
		for _, l := range lines[s:e] {
			if regexp.MustCompile(`code:\s*["']?` + regexp.QuoteMeta(code) + `["']?\s*[,}]`).MatchString(l) {
				return cfg, false, nil
			}
		}
		last := s
		for i := s + 1; i < e; i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "-") {
				last = i
			}
		}
		lines = append(lines[:last+1], append([]string{row}, lines[last+1:]...)...)
		return strings.Join(lines, "\n"), true, nil
	}
	dl := strings.Split(defaults, "\n")
	ds, de := languagesBlock(dl)
	if ds < 0 {
		return "", false, errors.New("config/default.yaml 里没有 languages: 块，无法生成语言配置")
	}
	block := []string{"languages:"}
	for _, l := range dl[ds+1 : de] {
		if strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			block = append(block, l)
		}
	}
	block = append(block, row)
	out := strings.TrimRight(cfg, "\n")
	if out != "" {
		out += "\n"
	}
	return out + strings.Join(block, "\n") + "\n", true, nil
}

type i18nWrite struct {
	rel  string
	data []byte
}

func translateWrites(writes []i18nWrite, to string) error {
	base := coreai.Config{Enabled: true, BaseURL: os.Getenv("NEXADMIN_AI_BASE_URL"), APIKey: os.Getenv("NEXADMIN_AI_API_KEY"), Model: os.Getenv("NEXADMIN_AI_MODEL"), Timeout: 90 * time.Second, MaxTokens: 8192}
	if !base.Ready() {
		return errors.New("--translate 需要 NEXADMIN_AI_BASE_URL、NEXADMIN_AI_MODEL，以及公网模型所需的 NEXADMIN_AI_API_KEY")
	}
	gw := coreai.NewGateway(func(context.Context) (coreai.Config, error) { return base, nil }, nil, time.UTC)
	gw.Limiter = nil
	for i := range writes {
		var err error
		if writes[i].rel == "config/config.yaml" {
			continue // 配置中的 locale code、方向和显示名不能被模型改写。
		}
		switch filepath.Ext(writes[i].rel) {
		case ".json":
			writes[i].data, err = translateJSON(gw, writes[i].data, to)
		case ".yaml":
			writes[i].data, err = translateYAML(gw, writes[i].data, to)
		}
		if err != nil {
			return fmt.Errorf("翻译 %s: %w", writes[i].rel, err)
		}
	}
	return nil
}

func translateStrings(gw *coreai.Gateway, texts []string, to string) ([]string, error) {
	out := append([]string(nil), texts...)
	for start := 0; start < len(texts); start += coreai.MaxBatchItems {
		end := start + coreai.MaxBatchItems
		if end > len(texts) {
			end = len(texts)
		}
		got, err := gw.Translate(context.Background(), coreai.Caller{UserID: "nexadmin-cli", TenantID: "platform", Exempt: true}, coreai.TranslateRequest{Texts: texts[start:end], From: "en-US", To: to, Format: "text"})
		if err != nil {
			return nil, err
		}
		copy(out[start:end], got)
	}
	return out, nil
}

func translateJSON(gw *coreai.Gateway, raw []byte, to string) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var nodes []string
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, v := range t {
				if k != untranslatedKey {
					t[k] = walk(v)
				}
			}
		case []any:
			for i, v := range t {
				t[i] = walk(v)
			}
		case string:
			if strings.TrimSpace(t) != "" {
				nodes = append(nodes, t)
			}
		}
		return x
	}
	v = walk(v)
	got, err := translateStrings(gw, nodes, to)
	if err != nil {
		return nil, err
	}
	n := 0
	var put func(any) any
	put = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, v := range t {
				t[k] = put(v)
			}
		case []any:
			for i, v := range t {
				t[i] = put(v)
			}
		case string:
			if strings.TrimSpace(t) != "" && n < len(got) {
				r := got[n]
				n++
				return r
			}
		}
		return x
	}
	v = put(v)
	b, err := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), err
}

func translateYAML(gw *coreai.Gateway, raw []byte, to string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var nodes []string
	var walk func(*yaml.Node, bool)
	walk = func(n *yaml.Node, key bool) {
		if !key && n.Kind == yaml.ScalarNode && n.Tag == "!!str" && strings.TrimSpace(n.Value) != "" && n.Value != untranslatedKey {
			nodes = append(nodes, n.Value)
		}
		for i, c := range n.Content {
			childKey := n.Kind == yaml.MappingNode && i%2 == 0
			walk(c, childKey)
		}
	}
	walk(&doc, false)
	got, err := translateStrings(gw, nodes, to)
	if err != nil {
		return nil, err
	}
	i := 0
	var put func(*yaml.Node, bool)
	put = func(n *yaml.Node, key bool) {
		if !key && n.Kind == yaml.ScalarNode && n.Tag == "!!str" && strings.TrimSpace(n.Value) != "" && n.Value != untranslatedKey && i < len(got) {
			n.Value = got[i]
			i++
		}
		for j, c := range n.Content {
			childKey := n.Kind == yaml.MappingNode && j%2 == 0
			put(c, childKey)
		}
	}
	put(&doc, false)
	return yaml.Marshal(&doc)
}

// ---- 前端翻译壳（纯函数） ----

func mergeJSON(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeJSON(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// markUntranslated 在 JSON 对象文本最前面加 "_untranslated": true，保留原有键序与格式。
func markUntranslated(raw []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return []byte("{\n  \"" + untranslatedKey + "\": true\n}\n"), nil
	}
	s := string(raw)
	i := strings.IndexByte(s, '{')
	return []byte(s[:i+1] + "\n  \"" + untranslatedKey + "\": true," + s[i+1:]), nil
}

// ---- 扫描 ----

// walkLocaleFiles 列出 root/rel 下所有 locales/ 目录里扩展名为 ext 的文件（跳过 node_modules / dist / 隐藏目录）。
func walkLocaleFiles(root, rel, ext string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(rel)), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == "dist" || (strings.HasPrefix(n, ".") && p != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) == ext && filepath.Base(filepath.Dir(p)) == "locales" {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func localeCode(p string) string { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) }

func cmdI18nAdd(root string, args []string, out io.Writer) error {
	fl := newFlags("i18n add")
	name := fl.String("name", "", "语言显示名（默认按常见语言推断，否则用 locale 代码）")
	dir := fl.String("dir", "", "文字方向 ltr|rtl（默认按语言推断）")
	translate := fl.Bool("translate", false, "调用配置的 OpenAI 兼容模型翻译生成文件")
	var pos []string
	rest := args
	for len(rest) > 0 {
		if err := fl.Parse(rest); err != nil {
			return fmt.Errorf("%w\n%s", err, i18nUsage)
		}
		if fl.NArg() == 0 {
			break
		}
		pos = append(pos, fl.Arg(0))
		rest = fl.Args()[1:]
	}
	if len(pos) != 1 {
		return errors.New(i18nUsage)
	}
	code := pos[0]
	if !localeRe.MatchString(code) {
		return fmt.Errorf("非法的 locale %q（BCP-47，如 fr-FR）", code)
	}
	if *dir == "" {
		*dir = "ltr"
		if rtlRe.MatchString(code) {
			*dir = "rtl"
		}
	}
	if *dir != "ltr" && *dir != "rtl" {
		return fmt.Errorf("--dir 只能是 ltr 或 rtl: %q", *dir)
	}
	if *name == "" {
		if *name = knownLangs[code]; *name == "" {
			*name = code
		}
	}
	// 1. 先把所有要写的内容算好再落盘，失败不留半截。
	var writes []i18nWrite
	cfgPath := filepath.Join(root, "config", "config.yaml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	def, err := os.ReadFile(filepath.Join(root, "config", "default.yaml"))
	if err != nil {
		return fmt.Errorf("读取 config/default.yaml: %w", err)
	}
	newCfg, cfgChanged, err := addLanguageLine(string(cfg), string(def), code, *name, *dir)
	if err != nil {
		return err
	}
	if cfgChanged {
		writes = append(writes, i18nWrite{"config/config.yaml", []byte(newCfg)})
	}
	// 2. 后端：以 core/i18n/locales/en-US.yaml 为底
	beRel := "config/locales/" + code + ".yaml"
	if !market.Exists(filepath.Join(root, filepath.FromSlash(beRel))) {
		base, err := os.ReadFile(filepath.Join(root, "core", "i18n", "locales", "en-US.yaml"))
		if err != nil {
			return fmt.Errorf("读取 core/i18n/locales/en-US.yaml: %w", err)
		}
		hdr := "# " + untranslatedKey + ": 以 en-US 为底的占位，请翻译后删除本行（缺翻译不报错，会回退到默认语言）\n"
		writes = append(writes, i18nWrite{beRel, append([]byte(hdr), base...)})
	}
	// 3. 前端
	files, err := walkLocaleFiles(root, "web/src", ".json")
	if err != nil {
		return err
	}
	merged := map[string]any{}
	for _, f := range files {
		r, _ := filepath.Rel(root, f)
		rel := filepath.ToSlash(r)
		if localeCode(f) != "en-US" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if strings.HasPrefix(rel, "web/src/framework/") || rel == "web/src/locales/en-US.json" {
			// 框架区文件不动（升级会覆盖）：框架的文案合并进用户区的一份 web/src/locales/<locale>.json
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			mergeJSON(merged, m)
			continue
		}
		dst := filepath.ToSlash(filepath.Join(filepath.Dir(rel), code+".json"))
		if market.Exists(filepath.Join(root, filepath.FromSlash(dst))) {
			continue
		}
		data, err := markUntranslated(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		writes = append(writes, i18nWrite{dst, data})
	}
	if top := "web/src/locales/" + code + ".json"; !market.Exists(filepath.Join(root, filepath.FromSlash(top))) {
		merged[untranslatedKey] = true
		data, err := json.MarshalIndent(merged, "", "  ")
		if err != nil {
			return err
		}
		writes = append(writes, i18nWrite{top, append(data, '\n')})
	}
	if *translate && len(writes) > 0 {
		if err := translateWrites(writes, code); err != nil {
			return err
		}
	}
	for _, w := range writes {
		if err := writeRel(root, w.rel, w.data); err != nil {
			return err
		}
	}
	if len(writes) == 0 {
		fmt.Fprintf(out, "语言 %s 已存在，没有需要生成的文件\n", code)
		return nil
	}
	fmt.Fprintf(out, "已添加语言 %s（%s，%s）:\n", code, *name, *dir)
	for _, w := range writes {
		fmt.Fprintln(out, "  +", w.rel)
	}
	fmt.Fprintln(out, "翻译文件以 en-US 为底并标注 "+untranslatedKey+"；翻译完成后删除该标记。nexadmin i18n check 可查看缺失的 key。")
	return nil
}

// ---- check ----

type localeSet struct {
	keys         map[string]map[string]bool // locale -> 打平后的 key
	untranslated map[string][]string        // locale -> 仍带未翻译标记的文件
}

func newLocaleSet() *localeSet {
	return &localeSet{keys: map[string]map[string]bool{}, untranslated: map[string][]string{}}
}

func (s *localeSet) add(code string, keys []string) {
	if s.keys[code] == nil {
		s.keys[code] = map[string]bool{}
	}
	for _, k := range keys {
		s.keys[code][k] = true
	}
}

// flattenKeys 把嵌套结构打平成 a.b.c；以 _ 开头的顶层键是元信息，不算 key。
func flattenKeys(prefix string, v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			if prefix == "" && strings.HasPrefix(k, "_") {
				continue
			}
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flattenKeys(p, c, out)
		}
	case map[any]any:
		m := map[string]any{}
		for k, c := range t {
			m[fmt.Sprint(k)] = c
		}
		flattenKeys(prefix, m, out)
	default:
		if prefix != "" {
			*out = append(*out, prefix)
		}
	}
}

// collectLocales 扫描 rel 目录下的翻译文件；ext 为 .json 或 .yaml；无法解析的文件记入 warns 后跳过。
func collectLocales(root string, rels []string, ext string, warns *[]string) (*localeSet, error) {
	set := newLocaleSet()
	for _, rel := range rels {
		files, err := walkLocaleFiles(root, rel, ext)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			var doc map[string]any
			if ext == ".json" {
				err = json.Unmarshal(raw, &doc)
			} else {
				err = yaml.Unmarshal(raw, &doc)
			}
			r, _ := filepath.Rel(root, f)
			if err != nil {
				*warns = append(*warns, fmt.Sprintf("%s 无法解析，已跳过: %v", filepath.ToSlash(r), err))
				continue
			}
			var keys []string
			flattenKeys("", doc, &keys)
			code := localeCode(f)
			set.add(code, keys)
			if _, ok := doc[untranslatedKey]; ok || (ext == ".yaml" && strings.Contains(string(raw), "# "+untranslatedKey)) {
				set.untranslated[code] = append(set.untranslated[code], filepath.ToSlash(r))
			}
		}
	}
	return set, nil
}

// missingKeys 以各语言 key 的并集为基准，返回每种语言缺失的 key（已排序）。
func missingKeys(s *localeSet) map[string][]string {
	ref := map[string]bool{}
	for _, ks := range s.keys {
		for k := range ks {
			ref[k] = true
		}
	}
	out := map[string][]string{}
	for code, ks := range s.keys {
		var miss []string
		for k := range ref {
			if !ks[k] {
				miss = append(miss, k)
			}
		}
		sort.Strings(miss)
		out[code] = miss
	}
	return out
}

func reportMissing(out io.Writer, title string, s *localeSet, all bool) {
	miss := missingKeys(s)
	codes := make([]string, 0, len(miss))
	for c := range miss {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	fmt.Fprintf(out, "== %s ==\n", title)
	if len(codes) == 0 {
		fmt.Fprintln(out, "(没有翻译文件)")
		return
	}
	for _, c := range codes {
		note := ""
		if len(s.untranslated[c]) > 0 {
			note = fmt.Sprintf("，%d 个文件仍标注未翻译", len(s.untranslated[c]))
		}
		fmt.Fprintf(out, "%s: 共 %d 个 key，缺失 %d 个%s\n", c, len(s.keys[c]), len(miss[c]), note)
		for i, k := range miss[c] {
			if !all && i >= 20 {
				fmt.Fprintf(out, "  ... 另有 %d 个（--all 列出全部）\n", len(miss[c])-20)
				break
			}
			fmt.Fprintln(out, "  -", k)
		}
	}
}

// cmdI18nCheck 只提示，不作为闸：缺失 key 也返回 nil（退出码 0）。
func cmdI18nCheck(root string, args []string, out io.Writer) error {
	fl := newFlags("i18n check")
	all := fl.Bool("all", false, "列出全部缺失的 key（默认每种语言最多 20 个）")
	if err := fl.Parse(args); err != nil {
		return fmt.Errorf("%w\n%s", err, i18nUsage)
	}
	var warns []string
	fe, err := collectLocales(root, []string{"web/src"}, ".json", &warns)
	if err != nil {
		return err
	}
	be, err := collectLocales(root, []string{"core/i18n", "config/locales"}, ".yaml", &warns)
	if err != nil {
		return err
	}
	reportMissing(out, "前端 web/src/**/locales/*.json", fe, *all)
	reportMissing(out, "后端 core/i18n/locales + config/locales", be, *all)
	for _, w := range warns {
		fmt.Fprintln(out, "提示:", w)
	}
	return nil
}
