package exportcenter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

// fakeStore 只实现下载 / 查询用到的 Get，其余方法未实现（调用即 nil 解引用，说明测试越界了）。
type fakeStore struct {
	job.Store
	jobs map[idgen.ID]*job.Job
}

func (f *fakeStore) Get(_ context.Context, id idgen.ID) (*job.Job, error) {
	if j, ok := f.jobs[id]; ok {
		c := *j
		return &c, nil
	}
	return nil, job.ErrNotFound
}

func setup(t *testing.T) (*gin.Engine, *fakeStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	files, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Put(context.Background(), "jobs/1/a.xlsx", strings.NewReader("XLSXDATA"), 8, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	st := &fakeStore{jobs: map[idgen.ID]*job.Job{
		1: {OwnerID: 10, Type: "user.export", Status: job.StatusSuccess, ResultFile: "jobs/1/a.xlsx", ResultName: "用户 导出.xlsx", ResultMime: "application/vnd.ms-excel"},
		2: {OwnerID: 10, Type: "user.export", Status: job.StatusRunning},
	}}
	for id, j := range st.jobs {
		j.ID = id
	}
	m := job.New(job.Options{Store: st, Files: files})
	h := &handler{m: m}
	e := gin.New()
	as := func(uid int64, super bool) gin.HandlerFunc {
		return func(c *gin.Context) {
			auth.SetClaims(c, &auth.Claims{UserID: idgen.ID(uid), TenantID: 1, Super: super})
		}
	}
	for name, uid := range map[string]int64{"owner": 10, "other": 11} {
		g := e.Group("/" + name)
		g.Use(as(uid, false))
		g.GET("/jobs", h.list)
		g.GET("/jobs/:id/download", h.download)
	}
	sup := e.Group("/super")
	sup.Use(as(99, true))
	sup.GET("/jobs/:id/download", h.download)
	return e, st
}

func get(e *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestDownloadOwnerGetsAttachment(t *testing.T) {
	e, _ := setup(t)
	w := get(e, "/owner/jobs/1/download")
	if w.Code != 200 || w.Body.String() != "XLSXDATA" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=utf-8''") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", w.Header())
	}
}

func TestDownloadOtherUserGets404(t *testing.T) {
	e, _ := setup(t)
	if w := get(e, "/other/jobs/1/download"); w.Code != 404 {
		t.Fatalf("other user code = %d", w.Code)
	}
	if w := get(e, "/super/jobs/1/download"); w.Code != 200 {
		t.Fatalf("super code = %d", w.Code)
	}
}

func TestDownloadUnfinishedConflict(t *testing.T) {
	e, _ := setup(t)
	if w := get(e, "/owner/jobs/2/download"); w.Code != 409 {
		t.Fatalf("running job download code = %d", w.Code)
	}
	if w := get(e, "/owner/jobs/999/download"); w.Code != 404 {
		t.Fatalf("missing job code = %d", w.Code)
	}
}

func TestListAllRequiresAdmin(t *testing.T) {
	e, _ := setup(t)
	if w := get(e, "/owner/jobs?all=1"); w.Code != 403 {
		t.Fatalf("non-admin all=1 code = %d", w.Code)
	}
}
