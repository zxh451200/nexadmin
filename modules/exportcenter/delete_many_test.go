package exportcenter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

// fakeStore 在 handler_test.go 的 fakeStore 之上补上批量删用到的 Delete（会真的从 map 里摘掉行，
// 便于断言「越权的那条没被删」）。
type batchStore struct {
	job.Store
	jobs map[idgen.ID]*job.Job
}

func (f *batchStore) Get(_ context.Context, id idgen.ID) (*job.Job, error) {
	if j, ok := f.jobs[id]; ok {
		c := *j
		return &c, nil
	}
	return nil, job.ErrNotFound
}

func (f *batchStore) Delete(_ context.Context, id idgen.ID) (bool, error) {
	if j, ok := f.jobs[id]; ok && job.IsFinal(j.Status) {
		delete(f.jobs, id)
		return true, nil
	}
	return false, nil
}

func del(e *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, path, nil))
	return w
}

func hasFile(t *testing.T, st storage.Storage, key string) bool {
	t.Helper()
	obj, err := st.Get(context.Background(), key, nil)
	if err != nil {
		return false
	}
	// 必须关掉句柄，否则 Windows 上 t.TempDir 清理会失败
	_ = obj.Body.Close()
	return true
}

// batchEnv 造三条任务：1=自己的成功任务（带结果文件）、2=自己的执行中任务、3=别人的成功任务。
func batchEnv(t *testing.T) (*gin.Engine, *batchStore, storage.Storage) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jobs/1/a.xlsx", "jobs/3/c.xlsx"} {
		if err := files.Put(context.Background(), k, strings.NewReader("X"), 1, storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	st := &batchStore{jobs: map[idgen.ID]*job.Job{
		1: {OwnerID: 10, Type: "user.export", Status: job.StatusSuccess, ResultFile: "jobs/1/a.xlsx", ResultName: "a.xlsx"},
		2: {OwnerID: 10, Type: "user.export", Status: job.StatusRunning},
		3: {OwnerID: 11, Type: "user.export", Status: job.StatusSuccess, ResultFile: "jobs/3/c.xlsx", ResultName: "c.xlsx"},
	}}
	for id, j := range st.jobs {
		j.ID = id
	}
	h := &handler{m: job.New(job.Options{Store: st, Files: files, Log: zap.NewNop()})}
	e := gin.New()
	as := func(uid int64, super bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(uid), TenantID: 1, Super: super})
		}
	}
	for name, u := range map[string][2]int64{"owner": {10, 0}, "other": {11, 0}, "super": {99, 1}} {
		g := e.Group("/" + name)
		g.Use(as(u[0], u[1] == 1))
		g.DELETE("/jobs", h.removeBatch)
	}
	return e, st, files
}

// 删除任务必须连带删结果文件（与单条 DELETE /jobs/:id 同一条 Manager.Delete 路径）。
// 把批量里的 Manager.Delete 换成只删行，本用例必红。
func TestDeleteManyRemovesRowAndResultFile(t *testing.T) {
	e, st, files := batchEnv(t)
	w := del(e, "/owner/jobs?ids=1")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if _, alive := st.jobs[1]; alive {
		t.Error("job row still present")
	}
	if hasFile(t, files, "jobs/1/a.xlsx") {
		t.Error("result file still present")
	}
}

// 一批里只要有一条还没结束，就整批 409，已经校验通过的那条也不能提前删（不做「删掉能删的」）。
// 去掉 removeBatch 里的 IsFinal 预校验，本用例必红：job 1 会被先删掉，然后才在 2 上报错。
func TestDeleteManyRejectsWholeBatchWhenOneJobIsActive(t *testing.T) {
	e, st, files := batchEnv(t)
	w := del(e, "/owner/jobs?ids=1,2")
	if w.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s, want 409", w.Code, w.Body.String())
	}
	if _, alive := st.jobs[1]; !alive {
		t.Error("已结束的任务被提前删掉了（应整批拒绝）")
	}
	if !hasFile(t, files, "jobs/1/a.xlsx") {
		t.Error("已结束任务的结果文件被提前删掉了")
	}
}

// 别人的任务按不存在处理、不计入实删数：越权 id 混在自己的那几条里，只删自己看得见的。
func TestDeleteManySkipsJobsOfOtherOwners(t *testing.T) {
	e, st, files := batchEnv(t)
	w := del(e, "/owner/jobs?ids=1,3")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if _, alive := st.jobs[3]; !alive {
		t.Error("别人的任务被删了")
	}
	if !hasFile(t, files, "jobs/3/c.xlsx") {
		t.Error("别人的任务的结果文件被删了")
	}
	if body := w.Body.String(); body == "" || !containsDeleted(body, 1) {
		t.Fatalf("body = %q, want deleted:1", body)
	}
}

// 管理员（超管）看得到全部任务，批量删也按同一口径生效。
func TestDeleteManyAdminSeesAll(t *testing.T) {
	e, st, _ := batchEnv(t)
	w := del(e, "/super/jobs?ids=3")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if _, alive := st.jobs[3]; alive {
		t.Error("admin should delete another owner's finished job")
	}
}

// 一条都没命中报 404，不能静默返回 deleted=0。
func TestDeleteManyNotFoundWhenNothingMatches(t *testing.T) {
	e, st, files := batchEnv(t)
	if w := del(e, "/owner/jobs?ids=999"); w.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%s, want 404", w.Code, w.Body.String())
	}
	if len(st.jobs) != 3 || !hasFile(t, files, "jobs/1/a.xlsx") {
		t.Error("404 之后不该有任何删除")
	}
}

// ?ids= 缺失或非法一律 400：退化成「不带 ids 就删空任务」是最难发现的误删。
func TestDeleteManyRejectsMissingOrBadIDs(t *testing.T) {
	e, st, _ := batchEnv(t)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=1,abc", "?ids=0"} {
		if w := del(e, "/owner/jobs"+q); w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
	if len(st.jobs) != 3 {
		t.Errorf("bad ids 请求不该删掉任何任务，剩 %d 条", len(st.jobs))
	}
}

// GormStore 路径：真正发出去的 DELETE 必须带上「仅已结束」的 status 条件（同单条），
// 并且别人的任务连 DELETE 都不会发出（作用域在 Manager.Get 上）。
func TestDeleteManyIssuesGuardedDeletePerID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := job.New(job.Options{Store: job.NewGormStore(db), Files: files, Log: zap.NewNop()})
	h := &handler{m: m}
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/owner")
	g.Use(func(c *gin.Context) { auth.SetClaims(c, &auth.Claims{UserID: 10, TenantID: 1}) })
	g.DELETE("/jobs", h.removeBatch)

	cols := []string{"id", "owner_id", "status", "result_file"}
	// 预校验各读一次；Manager.Delete 自己也会再读一次（那条路径就是单条删除用的同一条），
	// 所以 id=1 一共两次 SELECT，然后才是带 status 条件的 DELETE。
	mock.ExpectQuery("SELECT \\* FROM `sys_job` WHERE id = \\? LIMIT \\?").
		WithArgs(int64(1), 1).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(int64(1), int64(10), job.StatusSuccess, "jobs/1/a.xlsx"))
	mock.ExpectQuery("SELECT \\* FROM `sys_job` WHERE id = \\? LIMIT \\?").
		WithArgs(int64(2), 1).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(int64(2), int64(99), job.StatusSuccess, "jobs/2/b.xlsx"))
	mock.ExpectQuery("SELECT \\* FROM `sys_job` WHERE id = \\? LIMIT \\?").
		WithArgs(int64(1), 1).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(int64(1), int64(10), job.StatusSuccess, "jobs/1/a.xlsx"))
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sys_job` WHERE id = \\? AND status IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(1), job.StatusSuccess, job.StatusFailed, job.StatusCanceled).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := del(e, "/owner/jobs?ids=1,2")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !containsDeleted(w.Body.String(), 1) {
		t.Fatalf("body = %q, want deleted:1（id=2 属于别人，不计入）", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func containsDeleted(body string, n int64) bool {
	var v struct {
		Code int `json:"code"`
		Data struct {
			Deleted int64 `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return false
	}
	return v.Code == 0 && v.Data.Deleted == n
}
