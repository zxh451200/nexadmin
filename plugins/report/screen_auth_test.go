package report

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func screenRow(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("FROM `plg_report_screen` WHERE tenant_id = \\? AND id = \\?").WithArgs(int64(7), int64(9), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "name", "remark", "config"}).
			AddRow(9, 7, "大屏", "", `{"items":[],"refreshSec":30}`))
}

const userRolesSQL = "SELECT ur.role_id FROM sys_user_role AS ur JOIN sys_role r ON r.id = ur.role_id AND r.status = \\? WHERE ur.user_id = \\? AND r.tenant_id = \\?"

// 大屏按角色授权：无授权角色 / 授权表里没有该屏 -> 404（与不存在一致）；有则放行；超管与管理者可看全部。
func TestScreenRoleAuthorization(t *testing.T) {
	svc, mock := newTestService(t)
	user := Viewer{UserID: 42, Scope: allViewer().Scope}

	screenRow(mock) // 1) 用户没有任何角色
	mock.ExpectQuery(userRolesSQL).WithArgs("1", int64(42), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"role_id"}))
	if _, err := svc.GetScreenView(tctx(7), 9, user); !isStatus(err, 404) {
		t.Fatalf("no roles: err = %v, want 404", err)
	}

	screenRow(mock) // 2) 有角色，但没有被授权给该屏
	mock.ExpectQuery(userRolesSQL).WithArgs("1", int64(42), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(100))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_report_screen_role` WHERE tenant_id = \\? AND \\(screen_id = \\? AND role_id IN \\(\\?\\)\\)").
		WithArgs(int64(7), int64(9), int64(100)).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	if _, err := svc.GetScreenView(tctx(7), 9, user); !isStatus(err, 404) {
		t.Fatalf("unauthorized role: err = %v, want 404", err)
	}

	screenRow(mock) // 3) 授权角色 -> 放行
	mock.ExpectQuery(userRolesSQL).WithArgs("1", int64(42), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(100))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `plg_report_screen_role`").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(1))
	if v, err := svc.GetScreenView(tctx(7), 9, user); err != nil || v.Name != "大屏" || v.Config.RefreshSec != 30 {
		t.Fatalf("authorized: %+v %v", v, err)
	}

	screenRow(mock) // 4) 管理者 / 超管不查角色
	if _, err := svc.GetScreenView(tctx(7), 9, Viewer{Manager: true, Scope: allViewer().Scope}); err != nil {
		t.Fatal(err)
	}
	screenRow(mock)
	if _, err := svc.GetScreenView(tctx(7), 9, Viewer{Super: true, Scope: allViewer().Scope}); err != nil {
		t.Fatal(err)
	}

	screenRow(mock) // 5) 角色读取失败 -> 拒绝，不放行；查大屏图表同样先过授权
	mock.ExpectQuery(userRolesSQL).WillReturnError(errors.New("table sys_user_role missing"))
	if _, err := svc.QueryScreenChart(tctx(7), 9, 11, user); err == nil {
		t.Fatal("role lookup failure must deny")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScreenValidation(t *testing.T) {
	svc, mock := newTestService(t)
	item := func(x, y, w, h int) ScreenItem { return ScreenItem{ChartID: "11", X: x, Y: y, W: w, H: h} }
	bad := []ScreenForm{
		{Name: "", Config: ScreenConfig{}},
		{Name: "s", Config: ScreenConfig{RefreshSec: 1}},
		{Name: "s", Config: ScreenConfig{RefreshSec: 99999}},
		{Name: "s", Config: ScreenConfig{Items: []ScreenItem{item(20, 0, 8, 4)}}},
		{Name: "s", Config: ScreenConfig{Items: []ScreenItem{item(0, 0, 0, 4)}}},
		{Name: "s", Config: ScreenConfig{Items: []ScreenItem{item(-1, 0, 4, 4)}}},
		{Name: "s", Config: ScreenConfig{Items: []ScreenItem{item(0, 0, 8, 4), item(4, 2, 8, 4)}}},
	}
	for i, f := range bad {
		if _, err := svc.SaveScreen(tctx(7), 0, f); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
