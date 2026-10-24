package config

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/pkg/idgen"
)

type containsArg string

func (c containsArg) Match(v driver.Value) bool {
	s, _ := v.(string)
	return strings.Contains(s, string(c))
}

// 向导把站点名写入设置页「站点」分区（sys_setting.section=site 的 JSON 里的 siteName），其余字段保持默认。
func TestWriteSiteNameCreatesSiteSection(t *testing.T) {
	s, mock := newUploadSvc(t)
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	db := s.db
	mock.ExpectQuery("FROM `sys_setting`").WillReturnRows(sqlmock.NewRows([]string{"id", "section", "content"}))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_setting`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_setting`").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), "site", containsArg(`"siteName":"我的后台"`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := (module{}).WriteSiteName(context.Background(), db, "我的后台"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteSiteNameIgnoresBlank(t *testing.T) {
	s, mock := newUploadSvc(t)
	if err := (module{}).WriteSiteName(context.Background(), s.db, "   "); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil { // 空名字不碰数据库
		t.Fatal(err)
	}
}
