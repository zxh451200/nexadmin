package user

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"nexadmin/core/job"
)

// 任务参数落库前不能含敏感字段：键名里不得出现 password / secret / token 等。
func TestExportParamsHaveNoSensitiveKeys(t *testing.T) {
	b, err := json.Marshal(exportParams{Keyword: "a", Status: "1", DeptID: "2", Gender: "1", CreatedFrom: "2026-01-01", CreatedTo: "2026-02-01", ActorID: 7, Super: true})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for k := range m {
		for _, w := range []string{"password", "passwd", "secret", "token", "apikey", "api_key", "credential"} {
			if strings.Contains(strings.ToLower(k), w) {
				t.Errorf("param key %q looks sensitive", k)
			}
		}
	}
	if q := (exportParams{Keyword: "k", CreatedTo: "2026-02-01"}).query(); q.Keyword != "k" || q.CreatedTo != "2026-02-01" {
		t.Errorf("query() = %+v", q)
	}
}

func TestSyncLimitDefault(t *testing.T) {
	if syncLimitOf(0) != 5000 || syncLimitOf(-1) != 5000 || syncLimitOf(100) != 100 {
		t.Fatal("syncLimitOf")
	}
}

func userRow(id int64, status string, super bool) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "tenant_id", "status", "is_super"}).AddRow(id, 1, status, super)
}

// 执行时按所有者重新取身份：提交时快照里的 Super 不被采信（降级后重试不能继续以超管导出）。
func TestOwnerActorIgnoresSnapshotSuper(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(7, statusOn, false))
	a, err := s.ownerActor(ctx, 7)
	if err != nil || a.Super || a.ID != 7 {
		t.Fatalf("actor = %+v err = %v", a, err)
	}
	mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(8, statusOn, true))
	if a, err = s.ownerActor(ctx, 8); err != nil || !a.Super {
		t.Fatalf("actor = %+v err = %v", a, err)
	}
}

// 所有者被删除 / 停用 / 缺失时拒绝导出。
func TestOwnerActorRejectsGoneOrDisabled(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	if _, err := s.ownerActor(ctx, 0); err == nil {
		t.Error("zero id should be rejected")
	}
	mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(7, statusOff, false))
	if _, err := s.ownerActor(ctx, 7); err == nil {
		t.Error("disabled owner should be rejected")
	}
	mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := s.ownerActor(ctx, 9); err == nil {
		t.Error("missing owner should be rejected")
	}
}

// 即使参数里被写成 super=true，非超管所有者的导出仍会走数据范围查询（sys_user_role）。
func TestExportJobAppliesOwnerScopeNotSnapshot(t *testing.T) {
	s, mock, ctx := newScopeSvc(t)
	raw, _ := json.Marshal(exportParams{ActorID: 7, Super: true})
	mock.ExpectQuery("FROM `sys_user` WHERE").WillReturnRows(userRow(7, statusOn, false))
	expectScope(mock, "deptAndChild")
	expectCount(mock, 0)
	_, _ = s.exportJob(ctx, &job.Job{Params: string(raw)}, func(int, string) {})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("owner scope not applied: %v", err)
	}
}
