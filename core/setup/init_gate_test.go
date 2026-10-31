package setup

// 初始化判定测试：IsInitialized 是「能不能进初始化流程」的唯一闸门。
//
// 真实库部分默认跳过；设置 NEXADMIN_IT_MYSQL_DSN 才执行，例如
//
//	NEXADMIN_IT_MYSQL_DSN='root@tcp(127.0.0.1:3306)/' go test ./core/setup -run TestITIsInitialized -v

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/config"
)

type stubAdminChecker struct {
	has bool
	err error
}

func (s stubAdminChecker) HasAdmin(*gorm.DB) (bool, error) { return s.has, s.err }

func TestIsInitializedShortCircuits(t *testing.T) {
	log := zap.NewNop()
	dir := t.TempDir()
	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("app:\n  name: t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dir, UserConfigPath: existing}

	// 什么都没配置过 -> 未初始化
	cfg.UserConfigPath = filepath.Join(dir, "missing.yaml")
	if IsInitialized(cfg, stubAdminChecker{}, log) {
		t.Fatal("no user config must count as uninitialized")
	}
	cfg.UserConfigPath = existing

	// 标记文件存在 -> 已初始化，且不看数据库
	if err := os.WriteFile(MarkerPath(cfg), []byte("now"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !IsInitialized(cfg, stubAdminChecker{}, log) {
		t.Fatal("marker file must count as initialized")
	}
	_ = os.Remove(MarkerPath(cfg))

	// 没有管理员检查器时不能放行初始化（fail-closed）
	if !IsInitialized(cfg, nil, log) {
		t.Fatal("missing admin checker must stay fail-closed")
	}
}

func TestITIsInitializedMissingDatabase(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set; skipping real-MySQL test")
	}
	parsed, err := mysqldrv.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse NEXADMIN_IT_MYSQL_DSN: %v", err)
	}
	host, portStr, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		t.Fatalf("parse addr %q: %v", parsed.Addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	dir := t.TempDir()
	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("app:\n  name: t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: dir, UserConfigPath: existing}
	cfg.Database = config.Database{Type: "mysql", Host: host, Port: port, User: parsed.User, Password: parsed.Passwd}

	// 库不存在：init --config 与向导都带「库不存在则自动创建」，这里必须放行，否则全新库永远初始化不了。
	cfg.Database.Name = "nexadmin_nosuch_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if IsInitialized(cfg, stubAdminChecker{}, zap.NewNop()) {
		t.Fatal("a missing database must count as uninitialized")
	}

	// 负向对照：口令错（连不上）时仍必须按已初始化处理，不能把认证故障当成「可以重新初始化」。
	cfg.Database.Name = "mysql"
	cfg.Database.Password = parsed.Passwd + "!wrong"
	if !IsInitialized(cfg, stubAdminChecker{}, zap.NewNop()) {
		t.Fatal("authentication failure must stay fail-closed (treated as initialized)")
	}
}
