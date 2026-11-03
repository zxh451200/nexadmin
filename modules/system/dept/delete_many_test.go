package dept

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// 测试用的部门库形状：10 → 11 → 12 一条三层链，20 → 21 一条两层链，30 是孤立根。
var fixtureDepts = []struct {
	id   int64
	path string
}{
	{10, "/10/"},
	{11, "/10/11/"},
	{12, "/10/11/12/"},
	{20, "/20/"},
	{21, "/20/21/"},
	{30, "/30/"},
}

// fixtureRows 按 ID 白名单造 sys_dept 的查询结果；不传白名单就是完整库。
func fixtureRows(only ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "parent_id", "path"})
	for _, d := range fixtureDepts {
		keep := len(only) == 0
		for _, id := range only {
			if id == d.id {
				keep = true
			}
		}
		if keep {
			rows.AddRow(d.id, nil, d.path)
		}
	}
	return rows
}

func fixtureDeptsAll() []Dept {
	var out []Dept
	for _, d := range fixtureDepts {
		var row Dept
		row.ID = idgen.ID(d.id)
		row.Path = d.path
		out = append(out, row)
	}
	return out
}

func pathsOf(ids ...int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		for _, d := range fixtureDepts {
			if d.id == id {
				out = append(out, d.path)
			}
		}
	}
	return out
}

// 子树并集是「勾一个节点 = 删它和它全部后代」的纯逻辑，必须在没有数据库的地方就能测死，
// 否则前端确认框里的数字与真正被删掉的行数是否一致只能靠人肉点。
func TestSubtreeUnionIDs(t *testing.T) {
	all := fixtureDeptsAll()
	cases := []struct {
		name  string
		roots []int64
		want  []int64
	}{
		{"只勾叶子", []int64{21}, []int64{21}},
		{"父子同选不重复", []int64{10, 11}, []int64{10, 11, 12}},
		{"两棵不相交子树", []int64{10, 21}, []int64{10, 11, 12, 21}},
		{"中间节点只带自己的后代", []int64{11}, []int64{11, 12}},
		{"整棵子树加旁支", []int64{10, 21}, []int64{10, 11, 12, 21}},
		{"重复根不放大集合", []int64{20, 20}, []int64{20, 21}},
		{"空根集不得退化成全表", nil, []int64{}},
		{"库里没有的 ID", []int64{99}, []int64{}},
	}
	for _, c := range cases {
		got := subtreeUnionIDs(all, pathsOf(c.roots...))
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

// 级联并集要真的落到 SQL 上：IN 列表必须正好是并集，既不能漏后代，也不能把没选中的兄弟牵进来。
func TestDeleteManyDeletesUnionOfSubtrees(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(fixtureRows())
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user` WHERE deleted_at IS NULL AND dept_id IN \\(\\?,\\?,\\?,\\?\\)").
		WithArgs(int64(10), int64(11), int64(12), int64(21)).
		WillReturnRows(countRows(0))
	mock.ExpectExec("DELETE FROM `sys_dept` WHERE id IN \\(\\?,\\?,\\?,\\?\\)").
		WithArgs(int64(10), int64(11), int64(12), int64(21)).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	n, err := s.DeleteMany(tenant.With(context.Background(), 3), []idgen.ID{10, 11, 21})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if n != 4 {
		t.Fatalf("deleted = %d, want 4", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 与单条删除同一条护栏（service.go 里 Delete 的 users > 0 → dept.has_users）：
// 并集里任一部门还有未删除的用户就整体 409、一条都不删。
// 只数被勾选的那几个部门不够——子孙部门里的用户会悄悄失去部门归属。
func TestDeleteManyRejectedWhenAnyDescendantHasUsers(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(fixtureRows())
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_user` WHERE deleted_at IS NULL AND dept_id IN \\(\\?,\\?,\\?,\\?\\)").
		WithArgs(int64(10), int64(11), int64(12), int64(21)).
		WillReturnRows(countRows(5))
	mock.ExpectRollback()

	n, err := s.DeleteMany(tenant.With(context.Background(), 3), []idgen.ID{10, 11, 21})
	if n != 0 {
		t.Errorf("被拒绝的批量删除不该报出删除条数: %d", n)
	}
	key, arg := conflictKey(t, err)
	if key != "dept.has_users" || arg != int64(5) {
		t.Fatalf("key=%s n=%v", key, arg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 选中的 ID 在库里不存在（被并发删掉/脏数据）必须整体失败，不能静默少删：
// 静默少删会让返回的 deleted 与前端确认框里的选择集对不上。
func TestDeleteManyRejectsUnknownID(t *testing.T) {
	s, mock := newSvc(t)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `sys_dept`").WillReturnRows(fixtureRows(10, 11, 12))
	mock.ExpectRollback()

	n, err := s.DeleteMany(tenant.With(context.Background(), 3), []idgen.ID{10, 99})
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %#v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 空 ids 一旦被放过去，就是「不打对象的条件」（GORM 会渲染成 id IN (NULL)），
// 更糟的是有的调用方把它当「全选=全删」。必须当场 400，且一条 SQL 都不许发出去。
func TestDeleteManyRejectsEmptyIDs(t *testing.T) {
	s, mock := newSvc(t)
	n, err := s.DeleteMany(tenant.With(context.Background(), 3), nil)
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest || n != 0 {
		t.Fatalf("DeleteMany(nil) = (%d, %v)，想要 (0, 400)", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
