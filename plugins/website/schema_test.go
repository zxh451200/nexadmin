package website

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 字段视图与翻译白名单必须双向对齐：视图里有、白名单没有，前端会做出一个存不进库的输入框；
// 白名单里有、视图没有，那个字段就再也翻不了。两种漂移都能正常编译，只能靠这条测试兜住。
func TestEntityViewsMatchWhitelist(t *testing.T) {
	for entity, spec := range entityViews {
		inView := map[string]bool{}
		for _, v := range spec.views {
			if inView[v.key] {
				t.Errorf("%s: 字段 %s 在视图里出现两次", entity, v.key)
			}
			inView[v.key] = true
			if _, ok := spec.fields[v.key]; !ok {
				t.Errorf("%s: 视图字段 %s 不在翻译白名单里，填了也存不进库", entity, v.key)
			}
		}
		for key := range spec.fields {
			if !inView[key] {
				t.Errorf("%s: 白名单字段 %s 没有任何视图，界面上无法翻译", entity, key)
			}
		}
	}
}

// 富文本必须走清洗：视图标成 rich 而白名单没开 Rich，等于把未清洗的 HTML 送进库。
func TestRichFieldsAreSanitized(t *testing.T) {
	for entity, spec := range entityViews {
		for _, v := range spec.views {
			switch v.widget {
			case widgetText, widgetTextarea:
				if spec.fields[v.key].Rich {
					t.Errorf("%s.%s: 纯文本字段却开了清洗", entity, v.key)
				}
			case widgetRich:
				if !spec.fields[v.key].Rich {
					t.Errorf("%s.%s: 标成 rich 但白名单没开清洗", entity, v.key)
				}
			default:
				t.Errorf("%s.%s: 未知控件 %q", entity, v.key, v.widget)
			}
		}
	}
}

// labelKey 必须真的存在于两份插件文案里：i18next 取不到键时直接把键名渲染到界面上，
// 编译和测试都不会响，只有这条断言能挡住。
func TestSchemaLabelKeysExistInLocales(t *testing.T) {
	dicts := map[string]map[string]any{}
	for _, lang := range []string{"zh-CN", "en-US"} {
		path := filepath.Join("..", "..", "web", "src", "plugins", "website", "locales", lang+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读不到插件文案 %s（路径相对 plugins/website）：%v", path, err)
		}
		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatalf("%s 解析失败：%v", lang, err)
		}
		dicts[lang] = root
	}
	for entity, spec := range entityViews {
		for _, v := range spec.views {
			for lang, root := range dicts {
				if lookupKey(root, v.label) == nil {
					t.Errorf("%s.%s: 文案键 %s 在 %s 里不存在", entity, v.key, v.label, lang)
				}
			}
		}
	}
}

func lookupKey(root map[string]any, dotted string) any {
	var cur any = root
	for _, part := range strings.Split(dotted, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[part]
		if !ok {
			return nil
		}
	}
	return cur
}

func TestI18nSchemaFor(t *testing.T) {
	t.Run("未知实体", func(t *testing.T) {
		if _, ok := i18nSchemaFor("nope"); ok {
			t.Fatal("未知实体应当返回 false")
		}
	})

	t.Run("字段顺序与长度取自声明", func(t *testing.T) {
		s, ok := i18nSchemaFor("post")
		if !ok {
			t.Fatal("post 应当有 schema")
		}
		want := []string{"title", "summary", "seoDesc", "content"}
		if len(s.Translatable) != len(want) {
			t.Fatalf("字段数 = %d, want %d", len(s.Translatable), len(want))
		}
		for i, key := range want {
			if got := s.Translatable[i].Key; got != key {
				t.Errorf("第 %d 个字段 = %s, want %s", i, got, key)
			}
		}
		if got := s.Translatable[0].Max; got != 200 {
			t.Errorf("title.max = %d, want 200（应等于白名单长度）", got)
		}
		if got := s.Translatable[3].Widget; got != widgetRich {
			t.Errorf("content.widget = %s, want %s", got, widgetRich)
		}
	})

	// 只有带 published 裸列的内容才谈得上「按语言发布」。
	t.Run("发布能力只对 Post/Page 成立", func(t *testing.T) {
		for _, e := range []string{"post", "page"} {
			s, _ := i18nSchemaFor(e)
			if !s.Publishable {
				t.Errorf("%s 应当可按语言发布", e)
			}
		}
		for _, e := range []string{"setting", "nav", "feature"} {
			s, _ := i18nSchemaFor(e)
			if s.Publishable {
				t.Errorf("%s 没有可下线的 URL，不该出现发布开关", e)
			}
		}
	})

	// 负控制：把 title 从白名单里摘掉，schema 必须少给一个字段，
	// 而不是做出一个「填了不保存」的输入框。
	t.Run("白名单缺字段时宁可不渲染", func(t *testing.T) {
		v, had := postI18nFields["title"]
		delete(postI18nFields, "title")
		defer func() {
			if had {
				postI18nFields["title"] = v
			}
		}()
		s, _ := i18nSchemaFor("post")
		if len(s.Translatable) != 3 {
			t.Fatalf("摘掉白名单字段后应只剩 3 个，实得 %d（守卫没生效）", len(s.Translatable))
		}
		for _, f := range s.Translatable {
			if f.Key == "title" {
				t.Error("被白名单剔除的 title 仍出现在 schema 里")
			}
		}
	})
}
