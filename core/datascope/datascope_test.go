package datascope

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/response"
)

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

func rolesRows(pairs ...any) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "data_scope"})
	for i := 0; i < len(pairs); i += 2 {
		r.AddRow(pairs[i], pairs[i+1])
	}
	return r
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name  string
		roles *sqlmock.Rows
		extra func(m sqlmock.Sqlmock)
		all   bool
		depts []int64
	}{
		{"all", rolesRows(1, "all"), nil, true, nil},
		{"dept", rolesRows(1, "dept"), nil, false, []int64{50}},
		{"deptAndChild", rolesRows(1, "deptAndChild"), func(m sqlmock.Sqlmock) {
			m.ExpectQuery("SELECT path FROM `sys_dept`").WillReturnRows(sqlmock.NewRows([]string{"path"}).AddRow("/1/50/"))
			m.ExpectQuery("SELECT `id` FROM `sys_dept` WHERE path LIKE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(50).AddRow(51))
		}, false, []int64{50, 51}},
		{"custom", rolesRows(7, "custom"), func(m sqlmock.Sqlmock) {
			m.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(70).AddRow(71))
		}, false, []int64{70, 71}},
		{"self 只有本人", rolesRows(1, "self"), nil, false, nil},
		{"多角色并集去重", rolesRows(1, "dept", 7, "custom"), func(m sqlmock.Sqlmock) {
			m.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50).AddRow(70))
		}, false, []int64{50, 70}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := mockDB(t)
			mock.ExpectQuery("FROM sys_user_role AS ur").WillReturnRows(tc.roles)
			if !tc.all {
				mock.ExpectQuery("SELECT dept_id FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50))
			}
			if tc.extra != nil {
				tc.extra(mock)
			}
			sc, err := Resolve(context.Background(), db, 100, false)
			if err != nil {
				t.Fatal(err)
			}
			if sc.All != tc.all || !reflect.DeepEqual(sc.Depts, tc.depts) {
				t.Errorf("得到 all=%v depts=%v，期望 all=%v depts=%v", sc.All, sc.Depts, tc.all, tc.depts)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestResolveSuperAndApp(t *testing.T) {
	db, mock := mockDB(t)
	if sc, err := Resolve(context.Background(), db, 1, true); err != nil || !sc.All {
		t.Fatalf("超管应为全部：%v %v", sc, err)
	}
	if sc, err := FromClaims(context.Background(), db, &auth.Claims{Principal: auth.PrincipalApp}); err != nil || sc.Limited() {
		t.Fatalf("应用主体不应受限：%v %v", sc, err)
	}
	_, err := FromClaims(context.Background(), db, nil)
	if re, ok := err.(*response.Error); !ok || re.Status != http.StatusUnauthorized {
		t.Fatalf("无令牌应 401：%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestScopeHelpers(t *testing.T) {
	var none *Scope
	if none.Limited() || !none.HasDept(5) {
		t.Error("nil 范围应不裁剪")
	}
	all := &Scope{All: true}
	if _, _, ok := all.UserIDCond("created_by"); ok {
		t.Error("全部范围不应产生条件")
	}
	sc := &Scope{UserID: 100, Depts: []int64{50}}
	if !sc.HasDept(50) || sc.HasDept(60) {
		t.Error("HasDept 判断错误")
	}
	if cond, args, ok := sc.UserIDCond("created_by"); !ok || len(args) != 2 || cond == "" {
		t.Errorf("UserIDCond = %q %v %v", cond, args, ok)
	}
	if _, args, ok := (&Scope{UserID: 100}).UsernameCond("operator"); !ok || len(args) != 1 {
		t.Errorf("仅本人时 UsernameCond 只绑本人：%v %v", args, ok)
	}
	if e := Forbidden(context.Background(), "datascope.resource.log", 3); e.Status != http.StatusForbidden || e.Key != "datascope.forbidden_batch" || len(e.Args) != 2 || e.Args[1] != 3 {
		t.Errorf("Forbidden = %+v", e)
	}
}

func TestResolvePresetAndCustom(t *testing.T) {
	db, mock := mockDB(t)
	mock.ExpectQuery("FROM sys_user_role AS ur").WillReturnRows(rolesRows(1, "self", 2, "dept", 7, "custom"))
	mock.ExpectQuery("SELECT dept_id FROM `sys_user`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(50))
	mock.ExpectQuery("FROM `sys_role_dept`").WillReturnRows(sqlmock.NewRows([]string{"dept_id"}).AddRow(70))
	sc, err := Resolve(context.Background(), db, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Preset != "dept" || !sc.Custom || !reflect.DeepEqual(sc.Depts, []int64{50, 70}) {
		t.Errorf("Preset=%q Custom=%v Depts=%v", sc.Preset, sc.Custom, sc.Depts)
	}
}
