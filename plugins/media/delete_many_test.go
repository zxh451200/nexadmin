package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func mockDB(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mk, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return newService(db), mk
}

func statusOf(err error) int {
	var re *response.Error
	if errors.As(err, &re) {
		return re.Status
	}
	return 0
}

// 批量删视频台账只删本机这一行（与单条 DeleteVideo 同口径：插件不落原片、不转码，
// 厂商侧资产与单条一样需要在厂商控制台清理），所以这里钉的是「只动点名的 id」这一条：
// 去掉 Where("id IN ?") 就是清表，语句形状匹配不上直接红。
// 租户隔离由 core/database 的 tenantPlugin 在 DELETE 前追加 tenant_id 条件（与单条同一条路径，
// 裸 gorm 的 sqlmock 里注册不上该插件，插件本身由 core/database 的用例保证）。
// sqlmock 只正则匹配、从不执行 SQL，「少了哪几行」只能按 RowsAffected 的桩值钉。
func TestDeleteVideosOnlyRequestedIDs(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_media_video` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := s.DeleteVideos(tenant.With(context.Background(), 1), []idgen.ID{11, 12})
	if err != nil {
		t.Fatalf("DeleteVideos: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 越权 / 已不存在的 id 删不到，也就不会被计入实删数：点名 3 条、命中 1 条时回执必须是 1。
func TestDeleteVideosCountsOnlyRealDeletes(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_media_video` WHERE id IN \\(\\?,\\?,\\?\\)").
		WithArgs(int64(11), int64(12), int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := s.DeleteVideos(tenant.With(context.Background(), 1), []idgen.ID{11, 12, 13})
	if err != nil {
		t.Fatalf("DeleteVideos: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d, want 1", n)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 0 命中报 not found，不能静默返回 deleted=0 让界面以为删成功了。
// 语句本身成功（0 行），not found 是在事务外判的，所以这里必须仍是 Commit 而不是 Rollback。
func TestDeleteVideosNotFoundWhenNothingMatches(t *testing.T) {
	s, mock := mockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_media_video` WHERE id IN \\(\\?\\)").
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if _, err := s.DeleteVideos(tenant.With(context.Background(), 1), []idgen.ID{99}); statusOf(err) != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ?ids= 缺失或任一项非法一律 400，且在碰到数据库之前就返回（svc 为 nil：走到任何 SQL 都会 panic）。
func TestDeleteVideosHandlerRejectsMissingOrBadIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, q := range []string{"", "?ids=", "?ids=abc", "?ids=11,abc", "?ids=0", "?ids=-1"} {
		e := gin.New()
		h := &handler{svc: nil}
		e.DELETE("/plugins/media/video/assets", h.DeleteVideosBatch)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/media/video/assets"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: code=%d want 400 body=%s", q, w.Code, w.Body.String())
		}
	}
}

// 回执的 key 名字是前端读的字段（deleted），写成别的名字界面就永远提示「删除 0 条」。
func TestDeleteVideosHandlerReplyKey(t *testing.T) {
	s, mock := mockDB(t)
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.DELETE("/plugins/media/video/assets", func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.With(c.Request.Context(), 1))
		(&handler{svc: s}).DeleteVideosBatch(c)
	})
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `plg_media_video` WHERE id IN \\(\\?,\\?\\)").
		WithArgs(int64(11), int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/plugins/media/video/assets?ids=11,12", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `"deleted":2`) {
		t.Errorf("body = %q, want \"deleted\":2", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 集合路径与 :id 路径必须同时注册（gin 的路由树里两者共存，删掉任一条本用例红）：
// 前端批量走 /video/assets?ids=，单条走 /video/assets/:id。
func TestVideoRoutesKeepSingleAndBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api/v1")
	(&module{}).Register(api, &core.Deps{Config: &config.Config{}})
	got := map[string]bool{}
	for _, r := range engine.Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"DELETE /api/v1/plugins/media/video/assets",
		"DELETE /api/v1/plugins/media/video/assets/:id",
	} {
		if !got[want] {
			t.Errorf("缺少路由 %s（已注册 %v）", want, got)
		}
	}
}
