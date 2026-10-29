// Package i18n 是「一行内容的多语言」契约：译文文档、字段白名单、完成度与按语言发布。
//
// 它不认识任何具体内容，也不认识语言清单从哪来——后者由调用方通过 Locales 提供
// （官网是 sys_language，二开项目可以是任何来源）。
//
// 界面文案（key→value 的词条）不在这里：那是另一套体系（core/i18n + i18n-editor 插件）。
// 两者刻意分开——硬合成一张表会同时失去 schema 校验和 key 索引。
package i18n

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"nexadmin/pkg/htmlsafe"
)

// 译文与发布状态的体积上限：译文可能含富文本，发布状态只是布尔表。
const (
	MaxDocBytes   = 512 << 10
	MaxFlagsBytes = 8 << 10
)

// Language 是内容翻译关心的一门语言：只需要码和「是不是默认语言」。
type Language struct {
	Code    string
	Default bool
}

// Locales 由调用方实现。内容翻译需要知道「这门语言存不存在、是不是默认」，
// 但清单本身归调用方所有，否则 pkg 层就绑死了某个插件的表。
type Locales interface {
	// Resolve 按码解析出已配置语言，含 base-tag 回退（en-GB → en-US）。
	Resolve(code string) (Language, bool)
	// All 按配置顺序返回全部语言。
	All() []Language
}

// Field 是一个可翻译字段：Max=0 表示不限长度，Rich 表示按富文本清洗后再入库。
type Field struct {
	Max  int
	Rich bool
}

// Fields 是某类内容的可翻译字段白名单。不在白名单里的键一律丢弃。
type Fields map[string]Field

// Doc 是一行内容的译文：{"en-US":{"title":"…"}}。
// 默认语言不入内——它的正文就是行上的原文。
type Doc map[string]map[string]string

// ReviewKey 是「该语言的译文由机器翻译、还没人工校对」的保留键，与译文同住一个 JSON：
// {"en-US":{…},"_review":{"en-US":"mt"}}。它不是语言，所以不参与语言解析，也不进公开渲染。
const ReviewKey = "_review"

// ReviewMT 是 ReviewKey 下唯一合法的值；人工确认后该语言直接消失在清单里（不写 "ok"）。
const ReviewMT = "mt"

// ParseDoc 解析落库的译文 JSON。空串或损坏 JSON 返回 nil，
// 调用方据此整体回退原文：坏数据不该把页面打掉。
func ParseDoc(raw string) Doc {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var all Doc
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		return nil
	}
	// 校对状态是编辑期元数据：公开渲染只按语言取译文，不该把它当成一门语言。
	delete(all, ReviewKey)
	return all
}

// For 取某语言的字段译文。先按完整码精确取，取不到再按语基取
// （文档里存的是 "en" 而站点语言是 "en-US" 时的兼容路径）；都没有返回 nil。
func (d Doc) For(code string) map[string]string {
	if len(d) == 0 {
		return nil
	}
	if tr, ok := d[code]; ok {
		return tr
	}
	if base, _, found := strings.Cut(code, "-"); found {
		if tr, ok := d[base]; ok {
			return tr
		}
	}
	return nil
}

// Pick 取字段值：缺失或空串都回退原文。空串也算缺失，否则「填了又删干净」
// 会让该语言页面渲染成一片空白而不是退回原文。
func Pick(tr map[string]string, field, fallback string) string {
	if v, ok := tr[field]; ok && v != "" {
		return v
	}
	return fallback
}

// Normalize 校验并收窄译文：只保留已配置的非默认语言与白名单字段，
// 统一裁剪长度、清洗富文本、丢弃空值。无可留内容时返回空串。
//
// 语言键会被规范成清单里的码，所以前端传 "en" 也存成 "en-US"，读写口径一致。
func Normalize(raw string, locales Locales, fields Fields) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > MaxDocBytes {
		return "", fmt.Errorf("翻译内容过大")
	}
	var in map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("翻译 JSON 格式不正确")
	}
	out := map[string]map[string]string{}
	review := map[string]string{}
	for code, vals := range in {
		// 校对状态不是翻译内容：只认已配置的非默认语言、只认 "mt"，
		// 收窄规则和译文一致，否则客户端能塞任意码进来。
		if code == ReviewKey {
			for c, v := range vals {
				if l, ok := locales.Resolve(c); ok && !l.Default && v == ReviewMT {
					review[l.Code] = ReviewMT
				}
			}
			continue
		}
		l, ok := locales.Resolve(code)
		if !ok || l.Default {
			// 默认语言的正文就是原文，不必重复存译文。
			continue
		}
		dst := map[string]string{}
		for key, spec := range fields {
			v, ok := vals[key]
			if !ok {
				continue
			}
			if spec.Rich {
				v = htmlsafe.Sanitize(v)
			} else {
				v = strings.TrimSpace(v)
			}
			if spec.Max > 0 && utf8.RuneCountInString(v) > spec.Max {
				v = string([]rune(v)[:spec.Max])
			}
			if v != "" {
				dst[key] = v
			}
		}
		if len(dst) > 0 {
			out[l.Code] = dst
		}
	}
	if len(review) > 0 {
		out[ReviewKey] = review
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Translatable 报告某语言是否具备独立发布的内容：原文里非空的每个可翻译字段
// 都要有译文。原文本身为空的字段不算缺译——那种情况下各语言渲染结果一致，
// 没有「伪装成外语的原文页」要防。
func Translatable(code string, doc Doc, base map[string]string, fields Fields) bool {
	tr := doc.For(code)
	for key := range fields {
		if strings.TrimSpace(base[key]) == "" {
			continue
		}
		if strings.TrimSpace(tr[key]) == "" {
			return false
		}
	}
	return true
}

// NormalizePublish 把按语言的发布状态收窄成「所有已配置非默认语言各一条」的完整清单：
// 客户端没点名的语言沿用整行 published（升级前保存的内容不会因新规则突然下线），
// 缺译语言的 true 一律强制改成 false。
//
// 闸门必须在服务端：只在前端禁用开关的话，直接写接口就能把「伪装成日语的中文页」放回索引。
// 完成度按归一化后的译文重算，不信客户端的声明。
func NormalizePublish(raw string, published bool, doc Doc, base map[string]string, locales Locales, fields Fields) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > MaxFlagsBytes {
		return "", fmt.Errorf("发布状态过大")
	}
	var in map[string]bool
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("发布状态 JSON 格式不正确")
	}
	out := map[string]bool{}
	for _, l := range locales.All() {
		if l.Default {
			continue // 默认语言的发布态是行的 published 列，不重复存
		}
		on, ok := in[l.Code]
		if !ok {
			on = published
		}
		out[l.Code] = on && Translatable(l.Code, doc, base, fields)
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Published 报告某语言下这条内容是否可见。
//
// 状态文档为空表示从未按语言设置过发布（老数据，或不经新表单的写入）：沿用整行 published。
// 一旦启用过按语言发布，未点名的语言就是未发布——新加的语言在补齐译文前不该被索引。
// 坏数据按可见处理：解析失败不该顺手把已上线的页面打掉。
func Published(published bool, raw, code string, locales Locales) bool {
	if !published {
		return false // 原文下线 = 整篇下线
	}
	// 默认语言的发布态就是行的 published，永远不进文档；不先挡这一条，
	// 非空文档会把默认语言自己判成未发布。
	if l, ok := locales.Resolve(code); ok && l.Default {
		return true
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	var flags map[string]bool
	if err := json.Unmarshal([]byte(raw), &flags); err != nil {
		return true
	}
	on, ok := flags[code]
	if !ok {
		if base, _, found := strings.Cut(code, "-"); found {
			on, ok = flags[base]
		}
	}
	return ok && on
}
