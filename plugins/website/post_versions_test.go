package website

// 新闻版本历史的回归：归档守卫（内容相同不重复存）、发号（现有最大 version+1）、
// 滚动丢弃、列表不带正文、回滚取快照、删除级联清版本。
// 覆盖保存 → 归档 → 回滚的端到端链路在 post_versions_it_test.go（真引擎 TestIT*）钉；
// 这里用 sqlmock 把每一段的 SQL 形状与守卫条件钉死，改动一跑偏就红。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// mockDBNoTx 与 mockDB 同构，但关掉 gorm 默认事务：单条 Create/Update 不再包 Begin/Commit，
// 让归档链路的期望保持「一条语句一条期望」的可读数。
func mockDBNoTx(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, m
}

// ctxWithEditor 造一个带登录态的请求上下文：归档归属（created_by）从 claims 取。
func ctxWithEditor(username string) context.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	auth.SetClaims(c, &auth.Claims{UserID: 7, Username: username})
	return c.Request.Context()
}

func prevPost() *Post {
	pub := testNow.Add(-time.Hour)
	return &Post{
		PlatformModel: database.PlatformModel{ID: 1001, CreatedAt: testNow, UpdatedAt: testNow},
		Slug:          "launch", Title: "旧标题", Summary: "旧摘要", CoverURL: "cover.png",
		Content: "<p>旧正文 v1</p>", I18nJSON: `{"en-US":{"title":"old"}}`,
		Published: true, PublishedAt: &pub,
	}
}

// ---- 归档守卫与发号 ----

func TestArchivePostVersionSkipsUnchangedContent(t *testing.T) {
	db, m := mockDBNoTx(t)
	// 内容指纹相同：一条 SQL 都不该发出去（负控制靶子——删掉守卫这条就红）。
	v, err := archivePostVersion(context.Background(), db, prevPost(), "<p>旧正文 v1</p>")
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("内容未变应不归档，返回版本 %d", v)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Errorf("未变内容不应有 SQL 往来: %v", err)
	}
}

func TestArchivePostVersionStoresOldRowWithMaxPlusOne(t *testing.T) {
	db, m := mockDBNoTx(t)
	ctx := ctxWithEditor("editor-a")
	m.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM `plg_website_post_version`").
		WithArgs(int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(3))
	// 快照存的是旧行：标题/摘要/正文/译文都是 prev 的值，归属是正在保存的编辑。
	m.ExpectExec("INSERT INTO `plg_website_post_version`").
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), // id, created_at, updated_at
			int64(1001), 4, "旧标题", "旧摘要", "cover.png",
			"<p>旧正文 v1</p>", `{"en-US":{"title":"old"}}`,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			"editor-a",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	v, err := archivePostVersion(ctx, db, prevPost(), "<p>改坏了的正文</p>")
	if err != nil {
		t.Fatal(err)
	}
	if v != 4 {
		t.Errorf("版本号 = 现有最大+1，want 4, got %d", v)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestArchivePostVersionKeepsExactlyCapWithoutTrim(t *testing.T) {
	db, m := mockDBNoTx(t)
	// 成对边界：正好存到第 50 条不裁剪——只有 51 才删（上一条用例配对着钉）。
	m.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM `plg_website_post_version`").
		WithArgs(int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(maxPostVersions - 1))
	m.ExpectExec("INSERT INTO `plg_website_post_version`").
		WillReturnResult(sqlmock.NewResult(1, 1))
	v, err := archivePostVersion(context.Background(), db, prevPost(), "<p>第 50 版正文</p>")
	if err != nil {
		t.Fatal(err)
	}
	if v != maxPostVersions {
		t.Errorf("want %d, got %d", maxPostVersions, v)
	}
	// 若多发了一条 DELETE，sqlmock 会在这里报「not expected」。
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestArchivePostVersionRollsOldestBeyondCap(t *testing.T) {
	db, m := mockDBNoTx(t)
	m.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM `plg_website_post_version`").
		WithArgs(int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(maxPostVersions))
	m.ExpectExec("INSERT INTO `plg_website_post_version`").
		WillReturnResult(sqlmock.NewResult(1, 1))
	// 超限滚动丢弃最旧：插入 v51 后删掉 version <= 51-50 = 1 的行。
	m.ExpectExec("DELETE FROM `plg_website_post_version`").
		WithArgs(int64(1001), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	v, err := archivePostVersion(context.Background(), db, prevPost(), "<p>新正文</p>")
	if err != nil {
		t.Fatal(err)
	}
	if v != 51 {
		t.Errorf("want 51, got %d", v)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---- 列表形状：不带正文 ----

func TestListPostVersionsExcludesContent(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectQuery("SELECT .* FROM `plg_website_post`").
		WillReturnRows(sqlmock.NewRows([]string{"id", "slug", "title"}).AddRow(1001, "launch", "旧标题"))
	m.ExpectQuery("SELECT `version`,`title`,`summary`,`created_by`,`created_at` FROM `plg_website_post_version`").
		WillReturnRows(sqlmock.NewRows([]string{"version", "title", "summary", "created_by", "created_at"}).
			AddRow(2, "第二版", "摘要2", "editor-a", testNow).
			AddRow(1, "第一版", "摘要1", "editor-b", testNow.Add(-time.Hour)))
	list, err := NewService(db).ListPostVersions(context.Background(), 1001)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Version != 2 || list[1].Version != 1 {
		t.Fatalf("列表应新→旧: %+v", list)
	}
	// 序列化后不允许出现正文/译文键：mediumtext 绝不进列表响应。
	raw, _ := json.Marshal(list)
	for _, banned := range []string{"content", "i18n", "coverUrl", "published"} {
		if jsonHasKey(string(raw), banned) {
			t.Errorf("版本列表响应不应含 %q: %s", banned, raw)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func jsonHasKey(raw, key string) bool {
	var obj []map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return false
	}
	for _, o := range obj {
		if _, ok := o[key]; ok {
			return true
		}
	}
	return false
}

// ---- 单版本详情与回滚的取快照 ----

func TestGetPostVersionDetailReturnsContentAndWords(t *testing.T) {
	db, m := mockDBNoTx(t)
	m.ExpectQuery("SELECT .* FROM `plg_website_post_version`").
		WillReturnRows(sqlmock.NewRows([]string{"version", "title", "summary", "content", "created_by", "created_at"}).
			AddRow(1, "旧标题", "旧摘要", "<p>你好 世界</p>", "editor-a", testNow))
	d, err := NewService(db).GetPostVersion(context.Background(), 1001, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "<p>你好 世界</p>" {
		t.Errorf("详情应回传正文: %+v", d)
	}
	// 「你好 世界」= 5 个字符（含中间空格）：字数按剥掉 HTML 后的纯文本算。
	if d.Words != 5 {
		t.Errorf("words = %d, want 5", d.Words)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestGetPostVersionNotFound(t *testing.T) {
	db, m := mockDBNoTx(t)
	m.ExpectQuery("SELECT .* FROM `plg_website_post_version`").
		WillReturnRows(sqlmock.NewRows([]string{"version"}))
	_, err := NewService(db).GetPostVersion(context.Background(), 1001, 99)
	var be *response.Error
	if !errors.As(err, &be) || be.Key != "website.post_not_found" {
		t.Fatalf("want website.post_not_found 404, got %v", err)
	}
}

func TestRollbackPostVersionMissingSnapshotDoesNotTouchSave(t *testing.T) {
	db, m := mockDBNoTx(t)
	// 只发一条取快照的查询就应 404 返回：绝不能带着空快照走进 SavePost。
	m.ExpectQuery("SELECT .* FROM `plg_website_post_version`").
		WillReturnRows(sqlmock.NewRows([]string{"version"}))
	_, err := NewService(db).RollbackPostVersion(context.Background(), 1001, 99)
	var be *response.Error
	if !errors.As(err, &be) || be.Key != "website.post_not_found" {
		t.Fatalf("want website.post_not_found 404, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---- 删除级联清版本 ----

func TestDeletePostCascadesVersions(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_website_post`").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("DELETE FROM `plg_website_post_version`").
		WithArgs(int64(1001)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	m.ExpectCommit()
	if err := NewService(db).DeletePost(context.Background(), 1001); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDeletePostsBatchCascadesVersions(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_website_post`").
		WithArgs(int64(1001), int64(1002)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("DELETE FROM `plg_website_post_version`").
		WithArgs(int64(1001), int64(1002)).
		WillReturnResult(sqlmock.NewResult(0, 5))
	m.ExpectCommit()
	n, err := NewService(db).DeletePosts(context.Background(), []idgen.ID{1001, 1002})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// 一条都没删到：仍按原语义回 404，且不发级联删除（空命中不碰历史表）。
func TestDeletePostsNoneDeletedSkipsCascade(t *testing.T) {
	db, m := mockDB(t)
	m.ExpectBegin()
	m.ExpectExec("DELETE FROM `plg_website_post`").
		WithArgs(int64(404)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	m.ExpectCommit()
	_, err := NewService(db).DeletePosts(context.Background(), []idgen.ID{404})
	var be *response.Error
	if !errors.As(err, &be) || be.Key != "website.post_not_found" {
		t.Fatalf("want website.post_not_found 404, got %v", err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ---- 归档归属 ----

func TestEditorNameFromClaims(t *testing.T) {
	if got := editorName(context.Background()); got != "" {
		t.Errorf("无登录态应给空串: %q", got)
	}
	if got := editorName(ctxWithEditor("editor-a")); got != "editor-a" {
		t.Errorf("got %q", got)
	}
	// 用户名空的老令牌回退用户 ID 串，不写空归属。
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	auth.SetClaims(c, &auth.Claims{UserID: 7})
	if got := editorName(c.Request.Context()); got != "7" {
		t.Errorf("空用户名应回退 ID 串: %q", got)
	}
}
