package dict

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/pkg/importspec"
)

// 编译期保证：导入中心按 importspec.Provider 发现来源，字典模块必须实现它。
var _ importspec.Provider = module{}

func TestImportSourcesShape(t *testing.T) {
	s, _ := setup(t)
	srcs := module{}.ImportSources(s.db)
	if len(srcs) != 1 {
		t.Fatalf("want 1 source, got %d", len(srcs))
	}
	src := srcs[0]
	if src.Name != ImportSourceData || src.Handler == nil {
		t.Fatalf("bad source: %+v", src)
	}
	want := map[string]bool{"type_code": true, "label": true, "value": true}
	for _, c := range src.Columns {
		if c.Label == "" {
			t.Fatalf("column %q missing fallback label", c.Key)
		}
		delete(want, c.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing required-ish columns: %v", want)
	}
}

// 类型存在且值未占用 → 新建字典项。
func TestImportDataCreates(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\?").
		WillReturnRows(typeRow(9, "sys_common_status", "1"))
	mock.ExpectQuery("FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\?").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectGetType(mock, 9, typeRow(9, "sys_common_status", "1"))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\? AND id <> \\?").
		WithArgs(int64(9), "1", int64(0)).WillReturnRows(count(0))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_dict_data`").WillReturnResult(sqlmock.NewResult(4321, 1))
	mock.ExpectCommit()

	err := importData(bg, s, map[string]string{
		"type_code": "sys_common_status", "label": "成功", "value": "1", "tag": "success", "sort": "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 同类型同值已存在 → 走更新，不新建（重复导入不翻倍）。
func TestImportDataUpdatesExisting(t *testing.T) {
	s, mock := setup(t)
	dataCols := sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(20, 9, "1")
	mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\?").
		WillReturnRows(typeRow(9, "sys_common_status", "1"))
	mock.ExpectQuery("FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\?").WillReturnRows(dataCols)
	mock.ExpectQuery("FROM `sys_dict_data` WHERE id = \\?").WithArgs(20, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(20, 9, "1"))
	expectGetType(mock, 9, typeRow(9, "sys_common_status", "1"))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_dict_data` WHERE type_id = \\? AND dict_value = \\? AND id <> \\?").
		WithArgs(int64(9), "1", int64(20)).WillReturnRows(count(0))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_dict_data` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("FROM `sys_dict_data` WHERE id = \\?").WithArgs(20, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type_id", "dict_value"}).AddRow(20, 9, "1"))

	err := importData(bg, s, map[string]string{"type_code": "sys_common_status", "label": "成功", "value": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 类型编码写错 → 该行失败并给出人话，不碰数据表。
func TestImportDataRejectsUnknownType(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\?").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	err := importData(bg, s, map[string]string{"type_code": "nope", "label": "x", "value": "1"})
	if err == nil || !strings.Contains(err.Error(), "dict.type_not_found") {
		t.Fatalf("want type_not_found, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 排序不是整数 → 该行失败，且不落到后面的查询。
func TestImportDataRejectsBadSort(t *testing.T) {
	s, mock := setup(t)
	mock.ExpectQuery("FROM `sys_dict_type` WHERE code = \\?").
		WillReturnRows(typeRow(9, "sys_common_status", "1"))
	err := importData(bg, s, map[string]string{"type_code": "sys_common_status", "label": "x", "value": "1", "sort": "前十"})
	if err == nil || !strings.Contains(err.Error(), "imports.source.dict_data.sort_invalid") {
		t.Fatalf("want sort_invalid, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
