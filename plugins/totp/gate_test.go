package totp

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
)

// 查询失败必须拒绝登录（fail closed），不能把错误当成「未绑定」而跳过二次验证。
func TestGateFailsClosedOnQueryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT \* FROM ` + "`plg_totp_secret`").WillReturnError(errors.New("db down"))
	g := &gate{svc: NewService(db)}
	req, err := g.Check(context.Background(), auth.GateInput{User: &auth.User{ID: 7, TenantID: 1}})
	if err == nil || req != nil {
		t.Fatalf("query error must reject the login, got req=%+v err=%v", req, err)
	}
}
