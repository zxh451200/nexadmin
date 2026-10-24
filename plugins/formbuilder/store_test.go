package formbuilder

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 这些测试用 sqlmock 校验 gormStore 发出的 SQL 形状：每条语句都带 tenant_id，名额占用是单条原子 UPDATE，
// 唯一键冲突时回滚。没有真实数据库，行为（并发、唯一索引）由 memStore 的测试覆盖语义，此处只覆盖语句本身。
func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func met(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

var tctx = tenant.With(context.Background(), 7)

func init() { _ = idgen.Init(1) } // 框架启动时已初始化；单测里模拟

func TestStoreQueriesCarryTenant(t *testing.T) {
	db, mock := mockDB(t)
	st := NewStore(db)
	any1 := sqlmock.AnyArg()

	mock.ExpectQuery("FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(42), any1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id"}).AddRow(42, 7))
	if f, err := st.GetForm(tctx, 42); err != nil || f.TenantID != 7 {
		t.Fatalf("GetForm: %+v %v", f, err)
	}

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_formbuilder_form` WHERE tenant_id = \\?").WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	mock.ExpectQuery("FROM `plg_formbuilder_form` WHERE tenant_id = \\? ORDER BY id DESC LIMIT \\?").WithArgs(int64(7), 10).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, _, err := st.ListForms(tctx, FormQuery{Page: 1, PageSize: 10}); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND \\(status = \\? AND published_version > 0\\)").
		WithArgs(int64(7), StatusPublished, 200).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := st.ListPublished(tctx, 200); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("FROM `plg_formbuilder_version` WHERE tenant_id = \\? AND \\(form_id = \\? AND version = \\?\\)").
		WithArgs(int64(7), int64(42), 3, any1).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	if _, err := st.GetVersion(tctx, 42, 3); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id = \\? AND id < \\? ORDER BY id DESC LIMIT \\?").
		WithArgs(int64(7), int64(42), int64(900), 500).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := st.ScanSubmissions(tctx, SubQuery{FormID: 42}, 900, 500); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND \\(user_id = \\? AND form_id IN").
		WillReturnRows(sqlmock.NewRows([]string{"form_id"}).AddRow(42))
	if m, err := st.SubmittedForms(tctx, 5, []idgen.ID{42, 43}); err != nil || !m[42] || m[43] {
		t.Fatalf("SubmittedForms: %v %v", m, err)
	}
	met(t, mock)
}

func TestStoreWritesCarryTenant(t *testing.T) {
	db, mock := mockDB(t)
	st := NewStore(db)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `plg_formbuilder_form` SET .* WHERE tenant_id = \\? AND id = \\?").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := st.SaveDraft(tctx, 42, "n", "d", "{}"); err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `plg_formbuilder_form` SET .* WHERE tenant_id = \\? AND id = \\?").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := st.SaveAudience(tctx, 42, "{}"); !errors.Is(err, errNotFound) {
		t.Fatalf("更新 0 行应是 404（别的租户的表单不存在）: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id = \\?").WithArgs(int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_version` WHERE tenant_id = \\? AND form_id = \\?").WithArgs(int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := st.DeleteForm(tctx, 42); err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_formbuilder_submission` WHERE tenant_id = \\? AND form_id = \\? AND id = \\?").WithArgs(int64(7), int64(42), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `plg_formbuilder_form` SET `submission_count`=submission_count - 1 WHERE tenant_id = \\? AND id = \\? AND submission_count > 0").WithArgs(int64(7), int64(42)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := st.DeleteSubmission(tctx, 42, 9); err != nil {
		t.Fatal(err)
	}
	met(t, mock)
}

func TestStoreInsertSubmissionIsAtomicClaim(t *testing.T) {
	claim := "UPDATE `plg_formbuilder_form` SET `submission_count`=submission_count \\+ 1 WHERE tenant_id = \\? AND id = \\? AND status = \\? AND \\(\\? <= 0 OR submission_count < \\?\\)"
	sub := func() *Submission {
		return &Submission{FormID: 42, Version: 1, UserID: 5, Data: "{}", DedupeKey: "u:5"}
	}
	args := []driver.Value{int64(7), int64(42), StatusPublished, 3, 3}

	t.Run("成功", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(claim).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO `plg_formbuilder_submission`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		s := sub()
		if err := NewStore(db).InsertSubmission(tctx, s, 3); err != nil {
			t.Fatal(err)
		}
		if s.TenantID != 7 {
			t.Fatalf("提交应写入当前租户: %d", s.TenantID)
		}
		met(t, mock)
	})
	t.Run("名额已满", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(claim).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_formbuilder_form` WHERE tenant_id = \\? AND id = \\? AND status = \\?").
			WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
		mock.ExpectRollback()
		if err := NewStore(db).InsertSubmission(tctx, sub(), 3); !errors.Is(err, errStoreFull) {
			t.Fatalf("got %v", err)
		}
		met(t, mock)
	})
	t.Run("表单未发布或不在本租户", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(claim).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT count\\(\\*\\)").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
		mock.ExpectRollback()
		if err := NewStore(db).InsertSubmission(tctx, sub(), 3); !errors.Is(err, errStoreNotPublished) {
			t.Fatalf("got %v", err)
		}
		met(t, mock)
	})
	t.Run("唯一键冲突回滚（名额一并归还）", func(t *testing.T) {
		for _, msg := range []string{"Error 1062 (23000): Duplicate entry '1-2-u:5' for key", `ERROR: duplicate key value violates unique constraint "uk_x" (SQLSTATE 23505)`} {
			db, mock := mockDB(t)
			mock.ExpectBegin()
			mock.ExpectExec(claim).WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO `plg_formbuilder_submission`").WillReturnError(errors.New(msg))
			mock.ExpectRollback()
			if err := NewStore(db).InsertSubmission(tctx, sub(), 3); !errors.Is(err, errStoreDuplicate) {
				t.Fatalf("%s: got %v", msg, err)
			}
			met(t, mock)
		}
	})
}

func TestStorePublishIsOptimistic(t *testing.T) {
	upd := "UPDATE `plg_formbuilder_form` SET .* WHERE tenant_id = \\? AND id = \\? AND published_version = \\?"
	t.Run("并发发布落败", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(upd).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(7), int64(42), 1).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()
		err := NewStore(db).Publish(tctx, 42, &Version{Version: 2, Definition: "{}"}, PublishMeta{})
		if !errors.Is(err, errStoreConflict) {
			t.Fatalf("got %v", err)
		}
		met(t, mock)
	})
	t.Run("版本号唯一键兜底", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(upd).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO `plg_formbuilder_version`").WillReturnError(errors.New("Error 1062: Duplicate entry"))
		mock.ExpectRollback()
		err := NewStore(db).Publish(tctx, 42, &Version{Version: 2, Definition: "{}"}, PublishMeta{})
		if !errors.Is(err, errStoreConflict) {
			t.Fatalf("got %v", err)
		}
		met(t, mock)
	})
	t.Run("成功", func(t *testing.T) {
		db, mock := mockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(upd).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO `plg_formbuilder_version`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		v := &Version{Version: 2, Definition: "{}"}
		if err := NewStore(db).Publish(tctx, 42, v, PublishMeta{}); err != nil {
			t.Fatal(err)
		}
		if v.TenantID != 7 || v.FormID != 42 {
			t.Fatalf("版本应写入当前租户: %+v", v)
		}
		met(t, mock)
	})
}

// 没有租户上下文时，所有语句在到达数据库之前就被拒绝。
func TestStoreRejectsMissingTenant(t *testing.T) {
	db, mock := mockDB(t)
	st := NewStore(db)
	ctx := context.Background()
	calls := map[string]func() error{
		"CreateForm":        func() error { return st.CreateForm(ctx, &Form{}) },
		"GetForm":           func() error { _, e := st.GetForm(ctx, 1); return e },
		"ListForms":         func() error { _, _, e := st.ListForms(ctx, FormQuery{Page: 1, PageSize: 1}); return e },
		"ListPublished":     func() error { _, e := st.ListPublished(ctx, 1); return e },
		"SaveDraft":         func() error { return st.SaveDraft(ctx, 1, "", "", "") },
		"SaveAudience":      func() error { return st.SaveAudience(ctx, 1, "") },
		"DeleteForm":        func() error { return st.DeleteForm(ctx, 1) },
		"Publish":           func() error { return st.Publish(ctx, 1, &Version{Version: 1}, PublishMeta{}) },
		"SetStatus":         func() error { return st.SetStatus(ctx, 1, "a", "b") },
		"GetVersion":        func() error { _, e := st.GetVersion(ctx, 1, 1); return e },
		"ListVersions":      func() error { _, e := st.ListVersions(ctx, 1); return e },
		"InsertSubmission":  func() error { return st.InsertSubmission(ctx, &Submission{}, 0) },
		"FindByDedupe":      func() error { _, e := st.FindByDedupe(ctx, 1, "k"); return e },
		"GetSubmission":     func() error { _, e := st.GetSubmission(ctx, 1, 1); return e },
		"DeleteSubmission":  func() error { return st.DeleteSubmission(ctx, 1, 1) },
		"CountSubmissions":  func() error { _, e := st.CountSubmissions(ctx, SubQuery{}); return e },
		"PageSubmissions":   func() error { _, e := st.PageSubmissions(ctx, SubQuery{Page: 1, PageSize: 1}); return e },
		"ScanSubmissions":   func() error { _, e := st.ScanSubmissions(ctx, SubQuery{}, 0, 1); return e },
		"SubmittedForms":    func() error { _, e := st.SubmittedForms(ctx, 1, []idgen.ID{1}); return e },
		"Directory.Roles":   func() error { _, e := NewDirectory(db).Roles(ctx); return e },
		"Directory.Users":   func() error { _, e := NewDirectory(db).Users(ctx, "", 1); return e },
		"Directory.UserRol": func() error { _, e := NewDirectory(db).UserRoleIDs(ctx, 1); return e },
	}
	for name, fn := range calls {
		if err := fn(); !errors.Is(err, database.ErrNoTenant) {
			t.Errorf("%s: 无租户应返回 ErrNoTenant，实际 %v", name, err)
		}
	}
	met(t, mock) // 没有任何 SQL 发出
}

func TestDirectoryQueriesCarryTenant(t *testing.T) {
	db, mock := mockDB(t)
	d := NewDirectory(db)
	mock.ExpectQuery("JOIN sys_role r ON r.id = ur.role_id AND r.tenant_id = \\? AND r.status = \\?").WithArgs(int64(7), "1", int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(3).AddRow(4))
	ids, err := d.UserRoleIDs(tctx, 5)
	if err != nil || len(ids) != 2 || ids[0] != 3 {
		t.Fatalf("%v %v", ids, err)
	}
	mock.ExpectQuery("FROM `sys_user` WHERE tenant_id = \\? AND status = \\? AND deleted_at IS NULL").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := d.Users(tctx, "", 20); err != nil {
		t.Fatal(err)
	}
	met(t, mock)
}
