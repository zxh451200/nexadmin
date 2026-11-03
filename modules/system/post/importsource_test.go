package post

import (
	"context"
	"testing"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 导入源必须在碰到数据库之前就拒掉畸形行：
// 逐行导入是「一行失败只记那一行」，前面漏判就会让后面的落库半边成功，报告里说不清。
func TestImportPostRejectsBadRowsBeforeTouchingDB(t *testing.T) {
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	// svc.db 故意留 nil：真走到任何查询都会 panic，用例不 panic 就说明它被提前拒了
	svc := &Service{}
	ctx := tenant.With(context.Background(), idgen.ID(5))

	for _, row := range []map[string]string{
		{"code": "", "name": "总裁"},
		{"code": "  ", "name": "总裁"},
		{"code": "seo", "name": ""},
		{"code": "seo", "name": "  "},
		{"code": "seo", "name": "总裁", "sort": "1.5"},
		{"code": "seo", "name": "总裁", "sort": "abc"},
	} {
		if err := importPost(ctx, svc, row); err == nil {
			t.Errorf("row %v 应当被拒绝，却通过了", row)
		}
	}
}

// 声明的列必须与 handler 真正读取的 key 一一对应：模板里少了列，用户填了也进不来。
func TestImportSourceColumnsMatchHandler(t *testing.T) {
	src := (module{}).ImportSources(nil)[0]
	declared := map[string]bool{}
	for _, c := range src.Columns {
		declared[c.Key] = true
	}
	for _, key := range []string{"code", "name", "sort", "status", "remark"} {
		if !declared[key] {
			t.Errorf("handler 用了列 %q，但模板没声明", key)
		}
	}
	for _, c := range src.Columns {
		if c.Required && c.Example == "" {
			t.Errorf("必填列 %q 没有示例值，模板会教不会填", c.Key)
		}
		if c.LabelKey == "" {
			t.Errorf("列 %q 没有 LabelKey，非中文用户拿不到本地化表头", c.Key)
		}
	}
	if src.TitleKey == "" {
		t.Error("导入源标题没有 TitleKey")
	}
}
