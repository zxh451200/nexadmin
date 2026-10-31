package backup

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/auth"
	"nexadmin/core/config"
	"nexadmin/core/ctl"
	logmod "nexadmin/modules/log"
	"nexadmin/pkg/idgen"
)

var btestNow = time.Date(2026, 10, 17, 3, 30, 0, 0, time.UTC)

// okDumper 工具可用、导出即成功；配合 executeFn 空实现，测试不触发异步落库。
type okDumper struct{}

func (okDumper) Check() (string, error) { return "mysqldump", nil }
func (okDumper) Dump(context.Context, string) (ctl.DumpResult, error) {
	return ctl.DumpResult{Size: 1, SHA256: "x"}, nil
}
func (okDumper) Engine() string { return "mysql" }

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := idgen.Init(1); err != nil { // Start 要建雪花 ID 的备份记录
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		&gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return svcOn(db, t.TempDir()), mock
}

// svcOn 在给定 db 上建一个测试 Service（工具可用、时间固定、异步主体空实现）。
func svcOn(db *gorm.DB, dir string) *Service {
	cfg := &config.Config{}
	cfg.Backup.Dir = dir
	s := NewService(db, zap.NewNop(), cfg, "1.0.0")
	s.dumper = okDumper{}
	s.now = func() time.Time { return btestNow }
	s.executeFn = func(*Record) {} // 抑制异步主体，避免与同步断言抢 mock
	return s
}

// 抢到当天配额（哨兵行行锁内 count=0）才建 running 记录并提交。
func TestStartWinsInsertsRunning(t *testing.T) {
	s, mock := mockService(t)
	mock.MatchExpectationsInOrder(true)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_backup_setting`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `sys_backup_setting` WHERE .* FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settingID))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_backup`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("INSERT INTO `sys_backup`").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()

	rec, err := s.Start(context.Background(), SourceManual, idgen.ID(1))
	if err != nil || rec == nil || rec.Status != StatusRunning {
		t.Fatalf("rec=%+v err=%v", rec, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 已有未超时 running：行锁内 count>0 → 回滚，不建第二条记录，返回 ErrBusy。
func TestStartBusyRollbackNoInsert(t *testing.T) {
	s, mock := mockService(t)
	mock.MatchExpectationsInOrder(true)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_backup_setting`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `sys_backup_setting` WHERE .* FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settingID))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_backup`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	_, err := s.Start(context.Background(), SourceManual, idgen.ID(1))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 两个实例（共享库、各有本地 running 标志）并发只有一个能建出 running：第二个读到 running 即失败。
func TestTwoInstancesOnlyOneStarts(t *testing.T) {
	s1, mock := mockService(t)
	s2 := svcOn(s1.db, t.TempDir()) // 第二个实例：共享同一库/mock，独立 mu/running
	mock.MatchExpectationsInOrder(true)
	// 实例 1：count=0 → 插入 → 提交
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_backup_setting`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settingID))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_backup`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("INSERT INTO `sys_backup`").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()
	// 实例 2：行锁后读到刚插入的 running（count=1）→ 回滚，不再插入
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_backup_setting`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settingID))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_backup`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	if _, err := s1.Start(context.Background(), SourceManual, idgen.ID(1)); err != nil {
		t.Fatalf("winner: %v", err)
	}
	if _, err := s2.Start(context.Background(), SourceManual, idgen.ID(2)); !errors.Is(err, ErrBusy) {
		t.Fatalf("loser: want ErrBusy, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 定时：抢到日期后启动遇到瞬时 busy，必须释放当天抢占，让后续 tick 仍可重试。
func TestScheduledBusyReleasesDayClaim(t *testing.T) {
	s, mock := mockService(t)
	mock.MatchExpectationsInOrder(true)
	// Tick 读设置
	mock.ExpectQuery("SELECT .* FROM `sys_backup_setting` WHERE .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "schedule", "keep_days", "keep_count", "last_run_date"}).
			AddRow(settingID, "03:30", 7, 3, ""))
	// 抢日期（rows=1）
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_backup_setting` SET `last_run_date`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	// Start：确保哨兵行 → 事务 → count=1（别的实例在跑）→ 回滚
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sys_backup_setting`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settingID))
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `sys_backup`").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()
	// 释放当天抢占
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_backup_setting` SET `last_run_date`").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	s.Tick(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 定时：抢日期失败（别的实例已抢，rows=0）时不启动、也不释放。
func TestScheduledClaimLoserDoesNotStart(t *testing.T) {
	s, mock := mockService(t)
	mock.MatchExpectationsInOrder(true)
	mock.ExpectQuery("SELECT .* FROM `sys_backup_setting` WHERE .* LIMIT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "schedule", "keep_days", "keep_count", "last_run_date"}).
			AddRow(settingID, "03:30", 7, 3, ""))
	// 抢日期失败：rows=0
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `sys_backup_setting` SET `last_run_date`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	// 之后再无任何查询/写入。

	s.Tick(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 下载审计是同步写：调用返回时日志已经写入，而不是丢进一个不等退出的 goroutine。
func TestDownloadAuditIsSynchronous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, _ := mockService(t)
	written := make(chan *logmod.OperLog, 1)
	s.auditWrite = func(_ context.Context, l *logmod.OperLog) error { written <- l; return nil }
	h := NewHandler(s)

	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/api/v1/backups/99/download", nil)
	auth.SetClaims(c, &auth.Claims{Username: "admin", TenantID: 5})
	rec := &Record{FileName: "nexadmin-mysql-v1.0.0-20261017-030405.sql.gz"}
	rec.ID = idgen.ID(99)

	h.writeDownloadAudit(c, rec)

	select {
	case l := <-written:
		if l.Type != "download" || l.TenantID != idgen.ID(5) {
			t.Fatalf("bad audit row: %+v", l)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("download audit 未同步写入（疑似 fire-and-forget goroutine）")
	}
}
