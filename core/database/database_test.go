package database

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type tItem struct {
	TenantModel
	Name string
}

func (tItem) TableName() string { return "t_items" }

type pItem struct { // 平台级表：没有 tenant_id
	PlatformModel
	Name string
}

func (pItem) TableName() string { return "p_items" }

func newDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Use(tenantPlugin{}); err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"mysql": "mysql", "mariadb": "mysql", "postgres": "postgres", "postgresql": "postgres"} {
		if got, err := Normalize(in); err != nil || got != want {
			t.Errorf("Normalize(%q)=%q,%v 期望 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "sqlite", "MySQL", "oracle"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) 应报错", bad)
		}
	}
}

// 库名会被拼进 CREATE DATABASE，必须在连接之前就拒绝非法字符（防注入）。
func TestEnsureDatabaseRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", "a-b", "a b", "db`; DROP DATABASE x; --", `a"b`, "库", "a.b",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		err := EnsureDatabase(context.Background(), config.Database{Type: "mysql", Name: name})
		if err == nil {
			t.Errorf("库名 %q 应被拒绝", name)
		}
	}
}

func TestPlatformModelBeforeCreate(t *testing.T) {
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	m := &PlatformModel{}
	if err := m.BeforeCreate(nil); err != nil || m.ID == 0 {
		t.Fatalf("ID 为空应生成雪花 ID: id=%d err=%v", m.ID, err)
	}
	first := m.ID
	if err := m.BeforeCreate(nil); err != nil || m.ID != first {
		t.Fatalf("已有 ID 不能被覆盖: %d -> %d", first, m.ID)
	}
	a, b := &PlatformModel{}, &PlatformModel{}
	_ = a.BeforeCreate(nil)
	_ = b.BeforeCreate(nil)
	if a.ID == b.ID {
		t.Error("两次生成的 ID 不应相同")
	}
}

func TestTenantModelBeforeCreate(t *testing.T) {
	db, mock := newDB(t)
	insert := func(ctx context.Context, it *tItem) error {
		return db.WithContext(ctx).Create(it).Error
	}
	t.Run("自动填入当前租户和 ID", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `t_items`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		it := &tItem{Name: "a"}
		if err := insert(tenant.With(context.Background(), 7), it); err != nil {
			t.Fatal(err)
		}
		if it.TenantID != 7 || it.ID == 0 {
			t.Errorf("tenant=%d id=%d", it.TenantID, it.ID)
		}
	})
	t.Run("显式租户不被覆盖", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `t_items`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		it := &tItem{Name: "b", TenantModel: TenantModel{TenantID: 99}}
		if err := insert(tenant.With(context.Background(), 7), it); err != nil {
			t.Fatal(err)
		}
		if it.TenantID != 99 {
			t.Errorf("显式 tenant_id 被改成 %d", it.TenantID)
		}
	})
	t.Run("无租户 context 拒绝写入", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectRollback()
		it := &tItem{Name: "c"}
		if err := insert(context.Background(), it); !errors.Is(err, ErrNoTenant) {
			t.Fatalf("期望 ErrNoTenant，得到 %v", err)
		}
	})
	t.Run("Skip 放行且不填租户", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `t_items`").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		it := &tItem{Name: "d"}
		if err := insert(tenant.Skip(context.Background()), it); err != nil {
			t.Fatal(err)
		}
		if it.TenantID != 0 {
			t.Errorf("Skip 下不应自动填租户: %d", it.TenantID)
		}
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTenantScopeOnQueryUpdateDelete(t *testing.T) {
	db, mock := newDB(t)
	ctx := tenant.With(context.Background(), 5)

	mock.ExpectQuery("SELECT \\* FROM `t_items` WHERE .*`t_items`.`tenant_id` = \\?").WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	var items []tItem
	if err := db.WithContext(ctx).Find(&items).Error; err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `t_items` SET .* WHERE .*id = \\? AND `t_items`.`tenant_id` = \\?").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(1), int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := db.WithContext(ctx).Model(&tItem{}).Where("id = ?", 1).Update("name", "x").Error; err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `t_items` WHERE .*id = \\? AND `t_items`.`tenant_id` = \\?").
		WithArgs(int64(1), int64(5)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := db.WithContext(ctx).Where("id = ?", 1).Delete(&tItem{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTenantScopeRefusesWithoutTenant(t *testing.T) {
	db, mock := newDB(t) // 只预期事务的开启/回滚：被拒绝的语句本身不能到达数据库
	var items []tItem
	if err := db.Find(&items).Error; !errors.Is(err, ErrNoTenant) {
		t.Errorf("查询: 期望 ErrNoTenant，得到 %v", err)
	}
	mock.ExpectBegin()
	mock.ExpectRollback()
	if err := db.Model(&tItem{}).Where("id = 1").Update("name", "x").Error; !errors.Is(err, ErrNoTenant) {
		t.Errorf("更新: 期望 ErrNoTenant，得到 %v", err)
	}
	mock.ExpectBegin()
	mock.ExpectRollback()
	if err := db.Where("id = 1").Delete(&tItem{}).Error; !errors.Is(err, ErrNoTenant) {
		t.Errorf("删除: 期望 ErrNoTenant，得到 %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestTenantScopeSkipAndPlatformTables(t *testing.T) {
	db, mock := newDB(t)
	t.Run("Skip 不加租户条件", func(t *testing.T) {
		mock.ExpectQuery("SELECT \\* FROM `t_items`$").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		var items []tItem
		if err := db.WithContext(tenant.Skip(context.Background())).Find(&items).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("平台表无 tenant_id 列，无租户也能查且不加条件", func(t *testing.T) {
		mock.ExpectQuery("SELECT \\* FROM `p_items`$").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		var items []pItem
		if err := db.Find(&items).Error; err != nil {
			t.Fatal(err)
		}
	})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
