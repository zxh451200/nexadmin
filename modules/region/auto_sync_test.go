package region

import (
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/pkg/idgen"
)

func newAutoSyncEnv(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	if err := idgen.Init(1); err != nil {
		t.Fatal(err)
	}
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(db), mock
}

// TestAutoSyncLoopSkipsFreshSnapshot：距上次同步还没超过间隔，这轮什么都不做——
// 只查一次 synced_at，不发起同步（不会去碰数据源）。
func TestAutoSyncLoopSkipsFreshSnapshot(t *testing.T) {
	svc, mock := newAutoSyncEnv(t)
	mock.ExpectQuery("SELECT `synced_at` FROM `sys_region_snapshot`").
		WillReturnRows(sqlmock.NewRows([]string{"synced_at"}).AddRow(time.Now()))

	stop := make(chan struct{})
	close(stop) // 先跑首轮检查、再立即退出：ticker 永远等不到，进程里不留常驻协程
	autoSyncLoop(stop, svc, zap.NewNop(), 24, nil)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("fresh snapshot 不该发起同步，但查询对不上: %v", err)
	}
	if syncRunning() {
		t.Error("间隔未到却发起了同步")
	}
}

// TestAutoSyncLoopStartsWhenNeverSynced：从未同步过（在用内置数据）→ 到点发起一次后台同步。
// 测试里没配默认数据源，发起的同步会在提交阶段的廉价校验就被 400 拦下（source_missing），
// 不会真的联网；用「source_url 也查了一次」证明 StartSync 确实被调到了。
func TestAutoSyncLoopStartsWhenNeverSynced(t *testing.T) {
	svc, mock := newAutoSyncEnv(t)
	mock.ExpectQuery("SELECT `synced_at` FROM `sys_region_snapshot`").WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectQuery("SELECT `source_url` FROM `sys_region_snapshot`").WillReturnError(gorm.ErrRecordNotFound)

	stop := make(chan struct{})
	close(stop)
	autoSyncLoop(stop, svc, zap.NewNop(), 24, nil)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("从未同步过应该发起一次同步: %v", err)
	}
}

// TestAutoSyncLoopDisabled：hours<=0（出厂值 0）不开自动同步，连数据库都不该被摸到。
func TestAutoSyncLoopDisabled(t *testing.T) {
	svc, mock := newAutoSyncEnv(t)
	stop := make(chan struct{})
	close(stop)
	autoSyncLoop(stop, svc, zap.NewNop(), 0, nil)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("hours=0 不该有任何数据库活动: %v", err)
	}
}

// TestLastSyncedAtZeroTime：快照行存在但 synced_at 为零值（早期版本留下的行），
// 按「从未同步过」算，让自动同步有机会补上第一次。
func TestLastSyncedAtZeroTime(t *testing.T) {
	svc, mock := newAutoSyncEnv(t)
	mock.ExpectQuery("SELECT `synced_at` FROM `sys_region_snapshot`").
		WillReturnRows(sqlmock.NewRows([]string{"synced_at"}).AddRow(time.Time{}))

	_, ok, err := svc.lastSyncedAt(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("synced_at 为零值应该按从未同步处理")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
