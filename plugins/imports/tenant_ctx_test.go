package imports

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/i18n"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 大文件走异步协程时请求 ctx 已经不可用；租户只能从台账行还原，
// 否则租户表会在默认租户下落库（同步路径没有这个问题，容易漏测）。
func TestProcessAsyncRestoresTenant(t *testing.T) {
	var got idgen.ID
	var has bool
	var gotLang string
	src := Source{
		Name: "t_tenant", Title: "租户上下文",
		Columns: []Column{{Key: "k", Label: "K"}},
		Handler: func(ctx context.Context, _ int, _ map[string]string) error {
			got, has = tenant.From(ctx)
			gotLang = i18n.Lang(ctx)
			return nil
		},
	}
	db, mk := mockDB(t)
	s := NewService(db, nopLogger{})

	mk.ExpectExec("UPDATE `plg_imports_job`").WillReturnResult(sqlmock.NewResult(0, 1)) // status → running
	mk.ExpectExec("UPDATE `plg_imports_job`").WillReturnResult(sqlmock.NewResult(0, 1)) // 分批进度（末行）
	mk.ExpectExec("UPDATE `plg_imports_job`").WillReturnResult(sqlmock.NewResult(0, 1)) // 收尾

	m := &Job{Source: src.Name, TotalRows: 1, Status: StatusPending}
	m.TenantID = 42
	m.Lang = "en-US"
	s.processAsync(src, m, []map[string]string{{"k": "v"}}, "7")

	if !has || got != 42 {
		t.Fatalf("异步导入必须带上台账里的租户，实际 tenant=%v present=%v", got, has)
	}
	if gotLang != "en-US" {
		t.Fatalf("异步导入必须还原台账里的语言，实际 %q", gotLang)
	}
	// 台账的 UPDATE 次数随批次策略变化,这里只关心租户是否传到了 Handler。
	_ = mk
}
