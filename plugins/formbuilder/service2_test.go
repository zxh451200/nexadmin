package formbuilder

import (
	"bytes"
	"strings"
	"testing"

	"nexadmin/core/database"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

func TestTenantIsolation(t *testing.T) {
	s, _, _ := newSvc()
	id := publish(t, s, ctxT1, simpleDef("t1-form", fa))
	res, err := s.Submit(ctxT1, user(10), id, SubmitInput{Data: map[string]any{"a": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	// 租户 2 看不到、改不了、填不了、删不了租户 1 的任何东西
	checks := map[string]func() error{
		"GetForm":      func() error { _, e := s.GetForm(ctxT2, id); return e },
		"SaveDraft":    func() error { _, e := s.SaveDraft(ctxT2, id, SaveInput{Definition: simpleDef("x", fa)}); return e },
		"Publish":      func() error { _, e := s.Publish(ctxT2, admin, id); return e },
		"SetOnline":    func() error { _, e := s.SetOnline(ctxT2, id, false); return e },
		"DeleteForm":   func() error { return s.DeleteForm(ctxT2, id) },
		"ListVersions": func() error { _, e := s.ListVersions(ctxT2, id); return e },
		"GetVersion":   func() error { _, e := s.GetVersionDefinition(ctxT2, id, 1); return e },
		"FillGet":      func() error { _, e := s.FillGet(ctxT2, user(10), id); return e },
		"Submit": func() error {
			_, e := s.Submit(ctxT2, user(10), id, SubmitInput{Data: map[string]any{"a": "x"}})
			return e
		},
		"ListSubs":      func() error { _, e := s.ListSubmissions(ctxT2, SubQuery{FormID: id, Page: 1, PageSize: 10}); return e },
		"GetSubmission": func() error { _, e := s.GetSubmission(ctxT2, id, res.ID); return e },
		"DeleteSub":     func() error { return s.DeleteSubmission(ctxT2, id, res.ID) },
		"Export": func() error {
			_, e := s.WriteExport(ctxT2, &bytes.Buffer{}, SubQuery{FormID: id}, 10, "en-US")
			return e
		},
		"ExportDef": func() error { _, e := s.ExportDefinition(ctxT2, id, 0); return e },
	}
	for name, fn := range checks {
		if err := fn(); status(err) != 404 {
			t.Errorf("%s: 跨租户应得到 404，实际 %v", name, err)
		}
	}
	if list, total, _ := s.ListForms(ctxT2, FormQuery{Page: 1, PageSize: 10}); len(list) != 0 || total != 0 {
		t.Errorf("租户 2 的列表应为空: %v", list)
	}
	if items, _ := s.FillList(ctxT2, user(10)); len(items) != 0 {
		t.Errorf("租户 2 的填写列表应为空: %v", items)
	}
	// 租户 1 的数据完好
	if _, err := s.GetSubmission(ctxT1, id, res.ID); err != nil {
		t.Errorf("租户 1 的数据被破坏: %v", err)
	}
	// 没有租户上下文一律拒绝
	if _, err := s.GetForm(t.Context(), id); !errorsIs(err, database.ErrNoTenant) {
		t.Errorf("无租户应报 ErrNoTenant: %v", err)
	}
	if _, err := s.Submit(t.Context(), user(10), id, SubmitInput{}); !errorsIs(err, database.ErrNoTenant) {
		t.Errorf("无租户提交应报 ErrNoTenant: %v", err)
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestAudience(t *testing.T) {
	s, _, dir := newSvc()
	dir.roles[10] = []idgen.ID{5, 6}
	dir.roles[11] = []idgen.ID{7}
	id := publish(t, s, ctxT1, simpleDef("aud", fa))
	open := publish(t, s, ctxT1, simpleDef("open", fa))
	in := SubmitInput{Data: map[string]any{"a": "x"}}

	// 指定角色 5 + 用户 12
	if _, err := s.SaveDraft(ctxT1, id, SaveInput{Definition: simpleDef("aud", fa), Audience: &Audience{Roles: []string{"5"}, Users: []string{"12", "12"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctxT1, user(11), id, in); status(err) != 403 {
		t.Fatalf("不在范围内应 403: %v", err)
	}
	if _, err := s.FillGet(ctxT1, user(11), id); status(err) != 403 {
		t.Fatalf("不在范围内不能打开: %v", err)
	}
	if _, err := s.Submit(ctxT1, user(10), id, in); err != nil {
		t.Fatalf("拥有角色应可填: %v", err)
	}
	if _, err := s.Submit(ctxT1, user(12), id, in); err != nil {
		t.Fatalf("在用户名单里应可填: %v", err)
	}
	if _, err := s.Submit(ctxT1, admin, id, in); err != nil {
		t.Fatalf("超管不受范围限制: %v", err)
	}
	// 列表只显示可见的表单
	items, _ := s.FillList(ctxT1, user(11))
	if len(items) != 1 || items[0].ID != open {
		t.Fatalf("用户 11 只应看到不限范围的表单: %+v", items)
	}
	if n, _ := s.PendingCount(ctxT1, user(11)); n != 1 {
		t.Fatalf("待填数: %d", n)
	}
	// 读取角色失败 -> 拒绝而不是放行
	dir.failFor = map[idgen.ID]bool{13: true}
	if _, err := s.Submit(ctxT1, user(13), id, in); err == nil || status(err) == 0 && false {
		t.Fatalf("读取角色失败时必须拒绝")
	}
	if _, err := s.FillList(ctxT1, user(13)); err == nil {
		t.Fatalf("读取角色失败时列表应报错")
	}
	// 非法 ID 被拒绝
	if _, err := s.SaveDraft(ctxT1, id, SaveInput{Definition: simpleDef("aud", fa), Audience: &Audience{Roles: []string{"abc"}}}); status(err) != 400 {
		t.Fatalf("非法角色 ID: %v", err)
	}
	// 范围数据损坏 -> 拒绝
	f, _ := s.store.GetForm(ctxT1, id)
	f.Audience = "{not json"
	st := s.store.(*memStore)
	_ = st.SaveAudience(ctxT1, id, f.Audience)
	if _, err := s.Submit(ctxT1, user(10), id, in); status(err) != 403 {
		t.Fatalf("范围数据损坏应拒绝: %v", err)
	}
}

func TestDictSnapshot(t *testing.T) {
	s, _, dir := newSvc()
	dir.dicts["gender"] = []Option{{"男", "m"}, {"女", "f"}}
	d := simpleDef("dict", Field{Key: "g", Type: TypeSelect, Title: "G", DictType: "gender", Required: true})
	id := publish(t, s, ctxT1, d)
	// 字典后来变了，v1 仍按发布时的快照校验
	dir.dicts["gender"] = []Option{{"其他", "x"}}
	if _, err := s.Submit(ctxT1, user(1), id, SubmitInput{Data: map[string]any{"g": "m"}}); err != nil {
		t.Fatalf("快照内的值应可提交: %v", err)
	}
	if _, err := s.Submit(ctxT1, user(2), id, SubmitInput{Data: map[string]any{"g": "x"}}); status(err) != 400 {
		t.Fatalf("字典新增项不在 v1 快照里: %v", err)
	}
	fd, _ := s.FillGet(ctxT1, user(3), id)
	if len(fd.Definition.Fields[0].Options) != 2 {
		t.Fatalf("填写页应带快照选项: %+v", fd.Definition.Fields[0].Options)
	}
	// 字典为空 -> 不能发布
	dir.dicts["gender"] = nil
	if _, err := s.Publish(ctxT1, admin, id); status(err) != 400 {
		t.Fatalf("空字典不能发布: %v", err)
	}
}

func TestImportExportDefinition(t *testing.T) {
	s, _, _ := newSvc()
	d := simpleDef("exp", fa, Field{Key: "b", Type: TypeRadio, Title: "B", Options: opts("x", "y"), ShowIf: leaf("a", OpNotEmpty, nil)})
	d.Settings = Settings{AllowRepeat: true, MaxSubmissions: 9, SuccessMessage: "thx"}
	id := publish(t, s, ctxT1, d)
	_, _ = s.SaveDraft(ctxT1, id, SaveInput{Definition: d, Audience: &Audience{Roles: []string{"5"}}})
	env, err := s.ExportDefinition(ctxT1, id, 0)
	if err != nil || env.Format != EnvelopeFormat {
		t.Fatal(err)
	}
	raw := mustJSON(t, env)
	if strings.Contains(string(raw), `"audience"`) || strings.Contains(string(raw), `"roles"`) {
		t.Fatalf("导出文件不应包含可见范围: %s", raw)
	}
	// 导入到另一个租户：得到一个全新的草稿
	got, err := s.ImportDefinition(ctxT2, user(7), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDraft || got.PublishedVersion != 0 || len(got.Definition.Fields) != 2 || got.Definition.Settings.MaxSubmissions != 9 {
		t.Fatalf("导入结果不对: %+v", got)
	}
	if !got.Audience.Empty() {
		t.Fatalf("导入不带可见范围: %+v", got.Audience)
	}
	if got.ID == id {
		t.Fatal("导入应生成新表单")
	}
	// 也能导出已发布版本
	if _, err := s.ExportDefinition(ctxT1, id, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportDefinition(ctxT1, id, 99); status(err) != 404 {
		t.Fatalf("不存在的版本: %v", err)
	}
	// 恶意/损坏的导入文件
	bad := map[string]string{
		"不是 JSON":   `not json`,
		"格式标识不对":    `{"format":"other","schemaVersion":1,"definition":{"name":"x","fields":[]}}`,
		"版本不支持":     `{"format":"nexadmin.formbuilder","schemaVersion":99,"definition":{"name":"x","fields":[]}}`,
		"定义不合法":     `{"format":"nexadmin.formbuilder","schemaVersion":1,"definition":{"name":"x","fields":[{"key":"1","type":"text","title":"t"}]}}`,
		"正则不合法":     `{"format":"nexadmin.formbuilder","schemaVersion":1,"definition":{"name":"x","fields":[{"key":"a","type":"text","title":"t","pattern":"(a+"}]}}`,
		"条件执行代码的尝试": `{"format":"nexadmin.formbuilder","schemaVersion":1,"definition":{"name":"x","fields":[{"key":"a","type":"text","title":"t"},{"key":"b","type":"text","title":"t","showIf":{"field":"a","op":"js","value":"alert(1)"}}]}}`,
		"过大":        `{"format":"nexadmin.formbuilder","schemaVersion":1,"definition":{"name":"` + strings.Repeat("x", MaxDefBytes) + `","fields":[]}}`,
	}
	for name, body := range bad {
		if _, err := s.ImportDefinition(ctxT1, user(7), []byte(body)); status(err) != 400 {
			t.Errorf("%s: 应 400，实际 %v", name, err)
		}
	}
}

func TestSubmissionListFilterAndExport(t *testing.T) {
	s, _, _ := newSvc()
	d := simpleDef("exp",
		Field{Key: "name", Type: TypeText, Title: "姓名", Required: true},
		Field{Key: "color", Type: TypeCheckbox, Title: "颜色", Options: []Option{{"红", "r"}, {"蓝", "b"}}},
		Field{Key: "age", Type: TypeNumber, Title: "年龄"},
		Field{Key: "old", Type: TypeText, Title: "旧字段"},
	)
	d.Settings.AllowRepeat = true
	id := publish(t, s, ctxT1, d)
	submit := func(u int64, data map[string]any) {
		t.Helper()
		if _, err := s.Submit(ctxT1, user(u), id, SubmitInput{Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	submit(1, map[string]any{"name": "Alice", "color": []any{"r"}, "age": 30.0, "old": "keep"})
	submit(2, map[string]any{"name": "=HYPERLINK(\"http://evil\")", "color": []any{"r", "b"}, "age": 25.0})
	// v2 去掉 old、改标题
	d2 := simpleDef("exp", d.Fields[0], d.Fields[1], Field{Key: "age", Type: TypeNumber, Title: "岁数"})
	d2.Settings.AllowRepeat = true
	_, _ = s.SaveDraft(ctxT1, id, SaveInput{Definition: d2})
	if _, err := s.Publish(ctxT1, admin, id); err != nil {
		t.Fatal(err)
	}
	submit(3, map[string]any{"name": "Carol", "color": []any{"b"}, "age": 41.0})

	page, err := s.ListSubmissions(ctxT1, SubQuery{FormID: id, Page: 1, PageSize: 2})
	if err != nil || page.Total != 3 || len(page.List) != 2 || page.List[0].Username != "u3" {
		t.Fatalf("分页: %+v %v", page, err)
	}
	if page, _ = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Version: 1, Page: 1, PageSize: 10}); page.Total != 2 {
		t.Fatalf("按版本筛选: %d", page.Total)
	}
	if page, _ = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Field: "name", Value: "alic", Page: 1, PageSize: 10}); page.Total != 1 || page.List[0].Username != "u1" {
		t.Fatalf("按文本字段筛选: %+v", page)
	}
	if page, _ = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Field: "color", Value: "b", Page: 1, PageSize: 10}); page.Total != 2 {
		t.Fatalf("按多选字段筛选: %d", page.Total)
	}
	if page, _ = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Field: "age", Value: "41", Page: 1, PageSize: 10}); page.Total != 1 {
		t.Fatalf("按数字字段筛选: %d", page.Total)
	}
	if _, err = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Field: "a b;drop", Page: 1, PageSize: 10}); status(err) != 400 {
		t.Fatalf("非法字段名: %v", err)
	}
	if _, err = s.ListSubmissions(ctxT1, SubQuery{FormID: id, Field: "name", Value: strings.Repeat("x", 300), Page: 1, PageSize: 10}); status(err) != 400 {
		t.Fatalf("过长筛选值: %v", err)
	}

	var buf bytes.Buffer
	n, err := s.WriteExport(ctxT1, &buf, SubQuery{FormID: id}, 10, "zh-CN")
	if err != nil || n != 3 {
		t.Fatalf("导出: %d %v", n, err)
	}
	rows, err := excel.ReadAll(bytes.NewReader(buf.Bytes()), int64(buf.Len()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("应有表头 + 3 行: %v", rows)
	}
	header := strings.Join(rows[0], ",")
	// 所有版本字段的并集；标题取最新版本（岁数），已删除的 old 仍在
	if header != "提交ID,提交人,提交时间,版本,姓名,颜色,岁数,旧字段" {
		t.Fatalf("表头: %s", header)
	}
	// 最新在前：Carol(v2)，=HYPERLINK(v1)，Alice(v1)
	if rows[1][4] != "Carol" || rows[1][5] != "蓝" || rows[3][7] != "keep" {
		t.Fatalf("内容: %v", rows)
	}
	if rows[2][5] != "红、蓝" {
		t.Fatalf("多选应展示选项名: %v", rows[2])
	}
	if strings.HasPrefix(rows[2][4], "=") {
		t.Fatalf("公式注入未防护: %q", rows[2][4])
	}
	// 行数上限
	if _, err := s.WriteExport(ctxT1, &bytes.Buffer{}, SubQuery{FormID: id}, 2, "en-US"); status(err) != 400 {
		t.Fatalf("超过行数上限应报错: %v", err)
	}
}
