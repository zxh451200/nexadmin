package region

// 版本历史的单元测试：覆盖「覆盖保存留旧版」「内容没变不重复存」「列表不含整棵树」
// 「回滚走完整校验且版本继续递增」「恢复内置保留历史」。
// sqlmock 从不真的执行 SQL，所以这里只证明「语句按这个顺序发过、字段没漏」；
// 建表、版本单调、回滚写回的内容这些结构性断言在 it_test.go 的真库用例里。

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	coreregion "nexadmin/core/region"
	"nexadmin/pkg/idgen"
)

// snapshotColumns 是 GORM 读快照表一行时会映射的列（sqlmock 按列名对齐结构体字段）。
var snapshotColumns = []string{
	"id", "payload", "sha256", "source_url", "data_date", "license",
	"provinces", "prefectures", "counties", "synced_by", "synced_at",
}

// snapshotRow 造一行「当前快照」：payload 是即将被留档的旧树，sha 是它的摘要。
func snapshotRow(payload, sha, actor string, prov, pref, count int) []driver.Value {
	return []driver.Value{int64(1), payload, sha, "https://example.com/regions.json", "2026-01-02", "MIT", prov, pref, count, actor, time.Now()}
}

// snapshotModel 造一条「即将覆盖保存的新快照」：新树、新摘要、同步人是 actor——
// 与 snapshotRow 的默认来源/日期/量级一致，留档的 SHA 对比才有「内容变了要存」的语义。
func snapshotModel(payload, sha, actor string) Snapshot {
	return Snapshot{
		SourceURL: "https://example.com/regions.json",
		DataDate:  "2026-01-02",
		License:   "MIT",
		Payload:   payload,
		SHA256:    sha,
		Provinces: 34, Prefectures: 333, Counties: 2874,
		SyncedBy: actor,
		SyncedAt: time.Now(),
	}
}

// versionColumns 是历史版本表读取的列集合。
var versionColumns = []string{
	"id", "version", "payload", "sha256", "data_date", "source_url",
	"provinces", "prefectures", "counties", "created_by", "created_at", "updated_at",
}

// expectArchiveMiss 声明库里当前没有快照行（首次同步 / 刚恢复过内置）：覆盖前没有旧内容可留档。
func expectArchiveMiss(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot` WHERE id").
		WillReturnError(gorm.ErrRecordNotFound)
}

// expectArchiveFrom 声明「当前快照是这一行」，留档就会从它读出的内容写进历史表。
func expectArchiveFrom(mock sqlmock.Sqlmock, payload, sha, actor string) {
	row := snapshotRow(payload, sha, actor, 34, 333, 2874)
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot` WHERE id").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).AddRow(row...))
}

// expectMaxVersion 断言版本号从「现有最大值」推出来，而不是数行数或取列表下标。
func expectMaxVersion(mock sqlmock.Sqlmock, max int64) {
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\), 0\\) FROM `sys_region_snapshot_version`").
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(max))
}

func expectVersionInsert(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO `sys_region_snapshot_version`").WillReturnResult(sqlmock.NewResult(9, 1))
}

func expectSnapshotUpsert(mock sqlmock.Sqlmock, rowsAffected int64) {
	mock.ExpectExec("UPDATE `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(0, rowsAffected))
}

// newMockDB 起一对 sqlmock + GORM（不连真库）：只够断言「语句按这个顺序发过、字段没漏」，
// 从不真的执行 SQL。
func newMockDB(t *testing.T) (sqlmock.Sqlmock, *gorm.DB) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: gormlogger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return mock, db
}

// mustBuiltinTree 取随包发布的三级树，用它造「内置内容」与「改过的内容」。
func mustBuiltinTree(t *testing.T) []*coreregion.Node {
	t.Helper()
	tree, err := coreregion.Tree(3)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// TestSaveArchivesPreviousSnapshot 覆盖一行旧快照时，旧内容先变成一个版本。
func TestSaveArchivesPreviousSnapshot(t *testing.T) {
	mock, db := newMockDB(t)
	svc := NewService(db)
	ctx := context.Background()

	mock.ExpectBegin()
	expectArchiveFrom(mock, "旧树", "sha-old", "alice")
	expectMaxVersion(mock, 7)
	expectVersionInsert(mock)
	expectSnapshotUpsert(mock, 1)
	mock.ExpectCommit()

	if err := svc.save(ctx, Snapshot{Payload: "新树", SHA256: "sha-new", SyncedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestSaveSkipsUnchangedContent 内容与当前行一致（摘要相同）就不重复留档：
// 删掉 archiveCurrent 里那条 sha 相同的守卫，这里会多出一条没被期望的 INSERT，用例立刻红。
func TestSaveSkipsUnchangedContent(t *testing.T) {
	mock, db := newMockDB(t)
	svc := NewService(db)

	mock.ExpectBegin()
	expectArchiveFrom(mock, "同一棵树", "sha-same", "alice")
	expectSnapshotUpsert(mock, 1)
	mock.ExpectCommit()

	if err := svc.save(context.Background(), Snapshot{Payload: "同一棵树", SHA256: "sha-same", SyncedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestFirstSaveHasNothingToArchive 库里没有旧行时不写历史（内置数据不进历史表，它由代码常量决定）。
func TestFirstSaveHasNothingToArchive(t *testing.T) {
	mock, db := newMockDB(t)
	svc := NewService(db)

	mock.ExpectBegin()
	expectArchiveMiss(mock)
	expectSnapshotUpsert(mock, 0)
	mock.ExpectExec("INSERT INTO `sys_region_snapshot`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := svc.save(context.Background(), Snapshot{Payload: "新树", SHA256: "sha-new"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestVersionListHidesPayload 列表只给摘要字段，整棵树只在单版本详情里返回。
func TestVersionListHidesPayload(t *testing.T) {
	e, mock := newEnv(t)
	rows := sqlmock.NewRows(versionColumns).
		AddRow(int64(11), 7, "整棵树的 JSON", "sha-7", "2026-01-02", "https://example.com/r.json", 34, 333, 2874, "alice", time.Now(), time.Now()).
		AddRow(int64(12), 6, "另一棵树", "sha-6", "2025-11-01", "upload", 34, 332, 2870, "bob", time.Now(), time.Now())
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot_version` ORDER BY version DESC").WillReturnRows(rows)

	w := do(e, http.MethodGet, "/api/v1/regions/versions", "", "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("versions = %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "整棵树的 JSON") || strings.Contains(body, "payload") {
		t.Errorf("版本列表不该带出整棵树: %s", body)
	}
	if !strings.Contains(body, `"version":7`) || !strings.Contains(body, `"createdBy":"alice"`) || !strings.Contains(body, "2874") {
		t.Errorf("版本列表缺少展示字段: %s", body)
	}

	// 单版本详情才带正文。
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot_version` WHERE version").
		WillReturnRows(sqlmock.NewRows(versionColumns).
			AddRow(int64(11), 7, "整棵树的 JSON", "sha-7", "2026-01-02", "https://example.com/r.json", 34, 333, 2874, "alice", time.Now(), time.Now()))
	w = do(e, http.MethodGet, "/api/v1/regions/versions/7", "", "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("version detail = %d body=%s", w.Code, w.Body.String())
	}
	var detail struct {
		Data VersionDetail `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Data.Payload != "整棵树的 JSON" || detail.Data.Version != 7 || detail.Data.SHA256 != "sha-7" {
		t.Errorf("详情不对: %+v", detail.Data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestRollbackWritesBackHistory 回滚到历史版本：那份 payload 重新走完整校验与落库，
// 回滚前的当前快照同样留档（版本继续递增），历史表里那一条不会被删掉。
func TestRollbackWritesBackHistory(t *testing.T) {
	t.Cleanup(func() { coreregion.Reset() })

	e, mock := newEnv(t)
	builtin, err := coreregion.Encode(mustBuiltinTree(t))
	if err != nil {
		t.Fatal(err)
	}
	// 先把当前数据变成一份「和内置不同」的快照，回滚才有可见的变化。
	tree := mustBuiltinTree(t)
	tree[0].Name = "北京市回滚前"
	current, err := coreregion.Encode(tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coreregion.Activate(current, coreregion.Info{SourceURL: "https://example.com/old"}); err != nil {
		t.Fatal(err)
	}

	// 历史版本 v7 = 内置那份数据；当前行 = 回滚前的数据（摘要不同 → 覆盖前要留档）。
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot_version` WHERE version").
		WillReturnRows(sqlmock.NewRows(versionColumns).
			AddRow(int64(21), 7, string(builtin), "sha-7", "2026-01-02", "https://example.com/old", 34, 333, 2874, "alice", time.Now(), time.Now()))
	mock.ExpectBegin()
	expectArchiveFrom(mock, string(current), "sha-current", "carol")
	expectMaxVersion(mock, 7)
	expectVersionInsert(mock)
	expectSnapshotUpsert(mock, 1)
	mock.ExpectCommit()

	w := do(e, http.MethodPost, "/api/v1/regions/versions/7/rollback", "", "region:sync")
	if w.Code != http.StatusOK {
		t.Fatalf("rollback = %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data Result `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Info.Builtin || body.Data.Info.Provinces != 34 {
		t.Errorf("回滚结果不对: %+v", body.Data)
	}
	if name, _ := coreregion.Resolve("110000"); name != "北京市" {
		t.Errorf("回滚后生效的数据应是历史版本内容，得到 %q", name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestRollbackRejectsUnknownVersion 版本不存在时 404，不动当前数据。
func TestRollbackRejectsUnknownVersion(t *testing.T) {
	e, mock := newEnv(t)
	mock.ExpectQuery("SELECT \\* FROM `sys_region_snapshot_version` WHERE version").WillReturnError(gorm.ErrRecordNotFound)
	w := do(e, http.MethodPost, "/api/v1/regions/versions/99/rollback", "", "region:sync")
	if w.Code != http.StatusNotFound {
		t.Fatalf("回滚不存在的版本 = %d，期望 404（body=%s）", w.Code, w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// TestRollbackRejectsNonNumericVersion 版本号是路径参数：非正整数当场 400，不拼进 SQL。
func TestRollbackRejectsNonNumericVersion(t *testing.T) {
	e, _ := newEnv(t)
	for _, v := range []string{"abc", "0", "-3"} {
		if w := do(e, http.MethodPost, "/api/v1/regions/versions/"+v+"/rollback", "", "region:sync"); w.Code != http.StatusBadRequest {
			t.Errorf("version=%q 应 400，得到 %d", v, w.Code)
		}
	}
	if w := do(e, http.MethodGet, "/api/v1/regions/versions/abc", "", "region:sync"); w.Code != http.StatusBadRequest {
		t.Errorf("详情 version=abc 应 400，得到 %d", w.Code)
	}
}

// TestRollbackRefusesWhileSyncRunning 后台同步在跑时回滚一律 409：
// 两者改的是同一份内存快照，放行就会让回滚结果被在跑的同步覆盖掉。
func TestRollbackRefusesWhileSyncRunning(t *testing.T) {
	e, _ := newEnv(t)
	syncJob.Store(&syncState{Running: true, Stage: "fetching"})
	t.Cleanup(func() { syncJob.Store(&syncState{}) })

	if w := do(e, http.MethodPost, "/api/v1/regions/versions/1/rollback", "", "region:sync"); w.Code != http.StatusConflict {
		t.Fatalf("同步进行中的回滚 = %d，期望 409", w.Code)
	}
}

// TestVersionRoutesRequirePerm 版本历史接口沿用 region:sync：没有该权限一律 403。
func TestVersionRoutesRequirePerm(t *testing.T) {
	e, _ := newEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/regions/versions"},
		{http.MethodGet, "/api/v1/regions/versions/3"},
		{http.MethodPost, "/api/v1/regions/versions/3/rollback"},
	} {
		if w := do(e, tc.method, tc.path, "{}", "settings:list"); w.Code != http.StatusForbidden {
			t.Errorf("%s %s 无 region:sync = %d，期望 403", tc.method, tc.path, w.Code)
		}
	}
}
