package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
	"nexadmin/plugins/imports"
)

func testDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestDictSourceShape(t *testing.T) {
	src := dictSource(nil)
	if src.Name == "" || src.Title == "" || src.Handler == nil {
		t.Fatalf("导入源缺少 name/title/handler: %+v", src)
	}
	if err := imports.RegisterSource(src); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	seen := map[string]bool{}
	required := 0
	for _, c := range src.Columns {
		if c.Key == "" || c.Label == "" {
			t.Errorf("列缺少 key/label: %+v", c)
		}
		if seen[c.Key] {
			t.Errorf("列 key 重复: %s", c.Key)
		}
		seen[c.Key] = true
		if c.Required {
			required++
		}
	}
	if required != 3 {
		t.Errorf("必填列应为 字典编码/标签/值 三列，实际 %d", required)
	}
}

func TestNormalizeStatus(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		bad  bool
	}{
		{"", "1", false},
		{"1", "1", false},
		{"启用", "1", false},
		{"是", "1", false},
		{"Enabled", "1", false},
		{"0", "0", false},
		{"停用", "0", false},
		{"否", "0", false},
		{" false ", "0", false},
		{"待定", "", true},
	} {
		got, err := normalizeStatus(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("%q 应该报错，却得到 %q", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q → %q（err=%v），期望 %q", tc.in, got, err, tc.want)
		}
	}
}

func TestNormalizeSort(t *testing.T) {
	if n, err := normalizeSort("  "); err != nil || n != 0 {
		t.Errorf("空排序应得 0/无错，实际 %d/%v", n, err)
	}
	if n, err := normalizeSort("30"); err != nil || n != 30 {
		t.Errorf("30 应原样返回，实际 %d/%v", n, err)
	}
	if _, err := normalizeSort("-1"); err == nil {
		t.Error("负数排序应报错")
	}
	if _, err := normalizeSort("abc"); err == nil {
		t.Error("非数字排序应报错")
	}
}

func TestDictDataHandlerUpdatesExistingRow(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectQuery("SELECT \\* FROM `sys_dict_type` WHERE code = \\?").
		WithArgs("gender", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(9, "gender"))
	mock.ExpectQuery("SELECT \\* FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\?").
		WithArgs(int64(9), "1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "dict_value"}).AddRow(77, "1"))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_dict_data` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := dictDataHandler(db)(context.Background(), 2, map[string]string{
		"typeCode": "gender", "label": "男", "value": "1", "status": "启用",
	})
	if err != nil {
		t.Fatalf("覆盖已有字典项失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDictDataHandlerInsertsNewRow(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectQuery("SELECT \\* FROM `sys_dict_type` WHERE code = \\?").
		WithArgs("gender", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "code"}).AddRow(9, "gender"))
	mock.ExpectQuery("SELECT \\* FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\?").
		WithArgs(int64(9), "2", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(sort\\), 0\\) FROM `sys_dict_data`").
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(20))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_dict_data`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := dictDataHandler(db)(context.Background(), 3, map[string]string{
		"typeCode": "gender", "label": "女", "value": "2",
	})
	if err != nil {
		t.Fatalf("新增字典项失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDictDataHandlerRejectsUnknownType(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectQuery("SELECT \\* FROM `sys_dict_type` WHERE code = \\?").
		WithArgs("nope", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	err := dictDataHandler(db)(context.Background(), 1, map[string]string{
		"typeCode": "nope", "label": "x", "value": "1",
	})
	if err == nil {
		t.Fatal("字典类型不存在时应返回错误")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("错误信息应带上缺失的字典编码，实际: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
