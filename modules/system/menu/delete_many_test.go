package menu

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 测试用的菜单库形状：1(目录) → 2(菜单) → 4/5(按钮)；1 → 3(菜单) → 6(按钮)；7 是另一棵独立根。
func fixtureMenus() []Menu {
	mk := func(id, pid idgen.ID, typ string) Menu {
		m := Menu{ParentID: pid, Type: typ, Status: statusOn}
		m.PlatformModel = database.PlatformModel{ID: id}
		return m
	}
	return []Menu{
		mk(1, 0, typeDir),
		mk(2, 1, typeMenu),
		mk(3, 1, typeMenu),
		mk(4, 2, typeButton),
		mk(5, 2, typeButton),
		mk(6, 3, typeButton),
		mk(7, 0, typeMenu),
	}
}

func menuIDs(ids ...int64) []idgen.ID {
	out := make([]idgen.ID, 0, len(ids))
	for _, id := range ids {
		out = append(out, idgen.ID(id))
	}
	return out
}

// 与部门同一条口径：勾一个节点就是删它和它全部后代，选中集必须等于「真正会被删掉的行集合」。
// 这条并集是纯函数、不碰数据库，所以「不重不漏」可以在这里钉死。
func TestSubtreeUnion(t *testing.T) {
	rows := fixtureMenus()
	cases := []struct {
		name  string
		roots []int64
		want  []int64
	}{
		{"只勾叶子按钮", []int64{5}, []int64{5}},
		{"勾菜单带它的按钮", []int64{2}, []int64{2, 4, 5}},
		{"父子同选不重复", []int64{1, 2}, []int64{1, 2, 3, 4, 5, 6}},
		{"祖先与旁支同选", []int64{1, 6}, []int64{1, 2, 3, 4, 5, 6}},
		{"未选中的兄弟不牵进来", []int64{3}, []int64{3, 6}},
		{"重复根不放大集合", []int64{2, 2}, []int64{2, 4, 5}},
		{"空选择不得退化成全表", nil, []int64{}},
		{"库里没有的 ID 直接跳过", []int64{99}, []int64{}},
	}
	for _, c := range cases {
		got := subtreeUnion(rows, menuIDs(c.roots...))
		if len(got) != len(c.want) {
			t.Fatalf("%s: 得到 %v，想要 %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: 第 %d 项 %d != %d（整体 %v）", c.name, i, got[i], c.want[i], got)
			}
		}
	}
}

func menuFixtureRows() *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "parent_id", "type", "status"})
	for _, m := range fixtureMenus() {
		rows.AddRow(int64(m.ID), int64(m.ParentID), m.Type, m.Status)
	}
	return rows
}

// expectTreeLock 补上 mutateTree 的锚点行锁：批量删除与单条 remove 必须是同一把锁、同一锁序。
func expectTreeLock(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery("FROM `sys_menu` ORDER BY id ASC").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
}

// 批量删除的 SQL 必须是「并集一次删完 + 同一事务清角色授权」。
// IN 列表漏了后代就留孤儿节点，漏了 sys_role_menu 就留悬空 menu_id（角色授权树会解析到不存在的行）。
func TestRemoveManyDeletesUnionAndClearsRoleMenu(t *testing.T) {
	db, mock := mockDB(t)
	expectTreeLock(mock)
	mock.ExpectQuery("FROM `sys_menu` ORDER BY sort ASC, id ASC").WillReturnRows(menuFixtureRows())
	// 勾选 2、3、6：2→{2,4,5}、3→{3,6}、6→{6}，并集 5 条；1 与 7 没被勾选，不许进来
	mock.ExpectExec("DELETE FROM `sys_menu` WHERE id IN").
		WithArgs(int64(2), int64(3), int64(4), int64(5), int64(6)).
		WillReturnResult(sqlmock.NewResult(0, 5))
	mock.ExpectExec("DELETE FROM sys_role_menu WHERE menu_id IN").
		WithArgs(int64(2), int64(3), int64(4), int64(5), int64(6)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := (&service{db: db}).removeMany(context.Background(), menuIDs(2, 3, 6))
	if err != nil {
		t.Fatalf("removeMany: %v", err)
	}
	if n != 5 {
		t.Fatalf("deleted = %d, want 5", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 关联表清理失败必须整体回滚：留下「菜单没了、角色还授权着它」的中间态，
// 比拒绝这次删除危害大得多。
func TestRemoveManyRollsBackWhenRoleMenuCleanupFails(t *testing.T) {
	db, mock := mockDB(t)
	expectTreeLock(mock)
	mock.ExpectQuery("FROM `sys_menu` ORDER BY sort ASC, id ASC").WillReturnRows(menuFixtureRows())
	mock.ExpectExec("DELETE FROM `sys_menu` WHERE id IN").
		WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM sys_role_menu WHERE menu_id IN").
		WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	n, err := (&service{db: db}).removeMany(context.Background(), menuIDs(7))
	if err == nil {
		t.Fatal("清角色授权失败却被当成删除成功")
	}
	if n != 0 {
		t.Errorf("被回滚的删除不该报出条数: %d", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 选中的菜单全都不在库里（并发被删/脏 ID）时一条删除 SQL 都不能发出去：
// 空集合喂给 `id IN ?` 是不打对象的条件，历史语义里那等于全表删除。
func TestRemoveManyEmptyUnionIssuesNoDelete(t *testing.T) {
	db, mock := mockDB(t)
	expectTreeLock(mock)
	mock.ExpectQuery("FROM `sys_menu` ORDER BY sort ASC, id ASC").
		WillReturnRows(sqlmock.NewRows([]string{"id", "parent_id", "type", "status"}))
	mock.ExpectCommit()

	n, err := (&service{db: db}).removeMany(context.Background(), menuIDs(99))
	if err != nil {
		t.Fatalf("removeMany: %v", err)
	}
	if n != 0 {
		t.Fatalf("deleted = %d, want 0", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 服务层自己也要挡住空 ids：路由层的 ParseIDsQuery 只是第一道防线。
func TestRemoveManyRejectsEmptyIDs(t *testing.T) {
	db, mock := mockDB(t)
	var re *response.Error
	n, err := (&service{db: db}).removeMany(context.Background(), nil)
	if !errors.As(err, &re) || re.Status != 400 || n != 0 {
		t.Fatalf("removeMany(nil) = (%d, %v)，想要 (0, 400)", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
