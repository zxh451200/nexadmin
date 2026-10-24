package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Table 是版本表。
const Table = "sys_migration"

// legacyPluginTable 是升级前插件各自记录迁移的表（plugin_id + version），首次运行时作为基线导入。
const legacyPluginTable = "sys_plugin_migration"

// pgLockKey 是 Postgres 咨询锁的键（固定常量，ASCII "nexmig"）。
const pgLockKey int64 = 0x6e65786d6967

// Record 是版本表的一行。
type Record struct {
	Scope     string
	Version   string
	Checksum  string
	AppliedAt time.Time
	// Source 是记录来源：空表示迁移运行器真实执行；SourceManual 表示管理员手工标记（未执行其中的 SQL）。
	Source string
}

// SourceManual 是 `migrate --mark-applied` 写入的来源标记。
const SourceManual = "manual"

// sourceCol 是版本表里的来源列：可空、无默认值，老版本二进制的 INSERT（不含该列）照常工作。
const sourceCol = "source"

// Store 是运行器对数据库的全部依赖，便于用内存假实现做单测。
type Store interface {
	Engine() string // mysql | mariadb | postgres
	HasTable(name string) (bool, error)
	EnsureTable() error
	Applied() ([]Record, error)
	// Legacy 返回升级前以别的方式记录过的迁移文件名（目前只有插件的 sys_plugin_migration）。
	Legacy(scope string) (map[string]bool, error)
	// TryLock 非阻塞地尝试拿数据库级锁；ok=false 表示别的实例持有。
	TryLock() (unlock func(), ok bool, err error)
	// Atomic 报告执行一个文件是否可以整体回滚（Postgres 是，MySQL/MariaDB 的 DDL 不是）。
	Atomic() bool
	// Apply 依次执行 stmts 并写入 rec；返回已成功执行的语句条数。Atomic 为真时任何失败都整体回滚。
	Apply(rec Record, stmts []string) (done int, err error)
	// Insert 只写版本表一行，不执行任何 SQL（手工标记用）。
	Insert(rec Record) error
	// DeleteManual 删除一条来源为手工标记的记录；返回是否真的删除了（真实执行的记录永远不会被删）。
	DeleteManual(scope, version string) (bool, error)
}

// DetectEngine 返回 mysql | mariadb | postgres。MariaDB 与 MySQL 共用驱动，靠 VERSION() 区分。
func DetectEngine(db *gorm.DB) (string, error) {
	switch db.Dialector.Name() {
	case "postgres":
		return "postgres", nil
	case "mysql":
		var v string
		if err := db.Raw("SELECT VERSION()").Scan(&v).Error; err != nil {
			return "", fmt.Errorf("migrate: detect engine: %w", err)
		}
		if strings.Contains(strings.ToLower(v), "mariadb") {
			return "mariadb", nil
		}
		return "mysql", nil
	}
	return "", fmt.Errorf("migrate: unsupported dialect %q", db.Dialector.Name())
}

// GormStore 是基于 gorm 的真实实现。
type GormStore struct {
	db     *gorm.DB
	engine string
}

// NewGormStore 探测引擎并创建 Store。
func NewGormStore(db *gorm.DB) (*GormStore, error) {
	e, err := DetectEngine(db)
	if err != nil {
		return nil, err
	}
	return &GormStore{db: db, engine: e}, nil
}

func (s *GormStore) Engine() string { return s.engine }

func (s *GormStore) Atomic() bool { return s.engine == "postgres" }

func (s *GormStore) HasTable(name string) (bool, error) {
	return s.db.Migrator().HasTable(name), nil
}

// EnsureTable 建版本表；表已存在但缺 source 列（升级前建的库）时自动补列。
// 只用两种引擎都认的类型，仅时间类型按引擎区分。
func (s *GormStore) EnsureTable() error {
	if ok, _ := s.HasTable(Table); ok {
		if s.db.Migrator().HasColumn(Table, sourceCol) {
			return nil
		}
		return s.db.Exec("ALTER TABLE " + Table + " ADD COLUMN " + sourceCol + " VARCHAR(20) NULL").Error
	}
	ts := "DATETIME(3)"
	if s.engine == "postgres" {
		ts = "TIMESTAMP(3)"
	}
	return s.db.Exec(`CREATE TABLE ` + Table + ` (
  scope VARCHAR(100) NOT NULL,
  version VARCHAR(255) NOT NULL,
  checksum VARCHAR(64) NOT NULL,
  applied_at ` + ts + ` NOT NULL,
  ` + sourceCol + ` VARCHAR(20) NULL,
  PRIMARY KEY (scope, version)
)`).Error
}

// Applied 只读：缺 source 列时不查它（读路径不做 DDL），记录一律视为真实执行。
func (s *GormStore) Applied() ([]Record, error) {
	q := "SELECT scope, version, checksum, applied_at, COALESCE(" + sourceCol + ", '') AS source FROM " + Table
	if !s.db.Migrator().HasColumn(Table, sourceCol) {
		q = "SELECT scope, version, checksum, applied_at FROM " + Table
	}
	var rows []Record
	if err := s.db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *GormStore) Legacy(scope string) (map[string]bool, error) {
	id, ok := strings.CutPrefix(scope, ScopePrefix)
	if !ok || !s.db.Migrator().HasTable(legacyPluginTable) {
		return nil, nil
	}
	var vs []string
	if err := s.db.Raw("SELECT version FROM "+legacyPluginTable+" WHERE plugin_id = ?", id).Scan(&vs).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(vs))
	for _, v := range vs {
		out[v] = true
	}
	return out, nil
}

// TryLock 在一条专用连接上拿会话级锁：MySQL/MariaDB 用 GET_LOCK，Postgres 用 pg_try_advisory_lock。
func (s *GormStore) TryLock() (func(), bool, error) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, false, err
	}
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var (
		got    bool
		unlock func()
	)
	if s.engine == "postgres" {
		if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", pgLockKey).Scan(&got); err == nil {
			unlock = func() { _, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", pgLockKey) }
		}
	} else {
		var name string
		var n sql.NullInt64
		if err = conn.QueryRowContext(ctx, "SELECT CONCAT('nexadmin_migrate_', COALESCE(DATABASE(), ''))").Scan(&name); err == nil {
			if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&n); err == nil {
				got = n.Valid && n.Int64 == 1
				unlock = func() { _, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", name) }
			}
		}
	}
	if err != nil || !got {
		_ = conn.Close()
		return nil, false, err
	}
	return func() { unlock(); _ = conn.Close() }, true, nil
}

const insertRecord = "INSERT INTO " + Table + " (scope, version, checksum, applied_at) VALUES (?, ?, ?, ?)"

func (s *GormStore) Insert(rec Record) error {
	return s.db.Exec("INSERT INTO "+Table+" (scope, version, checksum, applied_at, "+sourceCol+") VALUES (?, ?, ?, ?, ?)",
		rec.Scope, rec.Version, rec.Checksum, rec.AppliedAt, rec.Source).Error
}

func (s *GormStore) DeleteManual(scope, version string) (bool, error) {
	if !s.db.Migrator().HasColumn(Table, sourceCol) {
		return false, nil
	}
	res := s.db.Exec("DELETE FROM "+Table+" WHERE scope = ? AND version = ? AND "+sourceCol+" = ?", scope, version, SourceManual)
	return res.RowsAffected > 0, res.Error
}

func (s *GormStore) Apply(rec Record, stmts []string) (int, error) {
	done := 0
	run := func(tx *gorm.DB) error {
		for _, st := range stmts {
			if err := tx.Exec(st).Error; err != nil {
				return err
			}
			done++
		}
		return tx.Exec(insertRecord, rec.Scope, rec.Version, rec.Checksum, rec.AppliedAt).Error
	}
	if s.Atomic() {
		return done, s.db.Transaction(run)
	}
	return done, run(s.db)
}
