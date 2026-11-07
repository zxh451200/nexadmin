package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
	"nexadmin/core/tenant"
)

// bizTables is the table boundary map used by most tests: biz_order and biz_item
// are tenant business tables; sys_user is a platform table.
func bizTables() map[string]bool {
	return map[string]bool{
		"biz_order": true,
		"biz_item":  true,
		"sys_user":  false,
	}
}

func TestSingleTableSQL(t *testing.T) {
	tables := bizTables()
	cases := []struct {
		name  string
		query string
		table string
		want  bool
	}{
		// --- positive: legitimate single-table DML ---
		{"select star", "SELECT * FROM biz_order WHERE id = ?", "biz_order", true},
		{"select columns", "SELECT id, name FROM biz_order WHERE id = ?", "biz_order", true},
		{"select order limit", "SELECT * FROM biz_order ORDER BY id LIMIT ?", "biz_order", true},
		{"select group having", "SELECT * FROM biz_order GROUP BY tenant_id HAVING COUNT(*) > ?", "biz_order", true},
		{"select in list", "SELECT * FROM biz_order WHERE id IN (?, ?, ?)", "biz_order", true},
		{"insert columns", "INSERT INTO biz_order (id, name) VALUES (?, ?)", "biz_order", true},
		{"insert values", "INSERT INTO biz_order VALUES (?, ?)", "biz_order", true},
		{"insert default", "INSERT INTO biz_order DEFAULT VALUES", "biz_order", true},
		{"update set", "UPDATE biz_order SET name = ? WHERE id = ?", "biz_order", true},
		{"upsert", "INSERT INTO biz_order (id, name) VALUES (?, ?) ON DUPLICATE KEY UPDATE name = ?", "biz_order", true},
		{"delete", "DELETE FROM biz_order WHERE id = ?", "biz_order", true},
		{"platform table", "SELECT * FROM sys_user WHERE id = ?", "sys_user", true},
		{"backtick table", "SELECT * FROM `biz_order` WHERE id = ?", "biz_order", true},
		{"same-boundary join", "SELECT * FROM biz_order JOIN biz_item ON biz_order.id = biz_item.order_id", "biz_order", true},
		{"returning", "DELETE FROM biz_order WHERE id = ? RETURNING id", "biz_order", true},

		// --- negative: multi-statement ---
		{"semicolon", "SELECT * FROM biz_order; DROP TABLE biz_order", "biz_order", false},
		{"trailing semicolon", "SELECT * FROM biz_order WHERE id = ?;", "biz_order", false},

		// --- negative: SQL comments ---
		{"hash comment", "SELECT * FROM biz_order # comment", "biz_order", false},
		{"dash comment", "SELECT * FROM biz_order -- comment", "biz_order", false},
		{"slash comment", "SELECT * FROM biz_order /* comment */", "biz_order", false},

		// --- negative: backslash (escape injection) ---
		{"backslash in string", "SELECT * FROM biz_order WHERE name = 'a\\'b'", "biz_order", false},

		// --- negative: cross-database qualifier ---
		{"cross-db select", "SELECT * FROM otherdb.biz_order", "biz_order", false},
		{"cross-db join", "SELECT * FROM biz_order JOIN otherdb.biz_item ON biz_order.id = biz_item.order_id", "biz_order", false},

		// --- negative: subquery ---
		{"subquery select", "SELECT * FROM biz_order WHERE id IN (SELECT id FROM biz_item)", "biz_order", false},
		{"subquery exists", "SELECT * FROM biz_order WHERE EXISTS (SELECT 1 FROM biz_item)", "biz_order", false},

		// --- negative: CTE ---
		{"cte", "WITH t AS (SELECT * FROM biz_order) SELECT * FROM t", "biz_order", false},

		// --- negative: UNION / set operations ---
		{"union", "SELECT * FROM biz_order UNION SELECT * FROM biz_item", "biz_order", false},
		{"intersect", "SELECT * FROM biz_order INTERSECT SELECT * FROM biz_item", "biz_order", false},
		{"except", "SELECT * FROM biz_order EXCEPT SELECT * FROM biz_item", "biz_order", false},

		// --- negative: unknown table ---
		{"unknown table", "SELECT * FROM unknown_table", "biz_order", false},
		{"target mismatch", "SELECT * FROM biz_item WHERE id = ?", "biz_order", false},

		// --- negative: cross-boundary JOIN ---
		{"join platform from business", "SELECT * FROM biz_order JOIN sys_user ON biz_order.id = sys_user.id", "biz_order", false},
		{"join business from platform", "SELECT * FROM sys_user JOIN biz_order ON sys_user.id = biz_order.id", "sys_user", false},

		// --- negative: comma table list ---
		{"comma tables", "SELECT * FROM biz_order, biz_item", "biz_order", false},

		// --- negative: non-DML ---
		{"drop", "DROP TABLE biz_order", "biz_order", false},
		{"truncate", "TRUNCATE biz_order", "biz_order", false},
		{"call", "CALL some_procedure()", "biz_order", false},
		{"load", "LOAD DATA INFILE '/tmp/x' INTO TABLE biz_order", "biz_order", false},
		{"outfile", "SELECT * FROM biz_order INTO OUTFILE '/tmp/x'", "biz_order", false},
		{"dumpfile", "SELECT * FROM biz_order INTO DUMPFILE '/tmp/x'", "biz_order", false},

		// --- negative: unclosed quote ---
		{"unclosed single quote", "SELECT * FROM biz_order WHERE name = 'abc", "biz_order", false},
		{"unclosed backtick", "SELECT * FROM `biz_order WHERE id = ?", "biz_order", false},

		// --- negative: too few tokens ---
		{"empty", "", "biz_order", false},
		{"select only", "SELECT", "biz_order", false},

		// --- negative: embedded UPDATE (not ON DUPLICATE KEY) ---
		{"embedded update", "INSERT INTO biz_order (id) VALUES (?) UPDATE biz_order SET name = ?", "biz_order", false},

		// --- negative: wrong INTO position ---
		{"into not insert", "SELECT * INTO biz_order FROM biz_item", "biz_order", false},

		// --- negative: multiple FROM ---
		{"double from", "SELECT * FROM biz_order FROM biz_item", "biz_order", false},

		// --- negative: alias after table (non-clause) ---
		{"select alias", "SELECT * FROM biz_order AS o WHERE o.id = ?", "biz_order", true},
		{"update alias", "UPDATE biz_order AS o SET o.name = ?", "biz_order", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := singleTableSQL(c.query, c.table, tables)
			if got != c.want {
				t.Errorf("singleTableSQL(%q, %q) = %v, want %v", c.query, c.table, got, c.want)
			}
		})
	}
}

func TestRoutingPoolSelectPool(t *testing.T) {
	p := &isolation{tables: bizTables(), tenants: map[string]*gorm.DB{}}
	platformDB, _, _ := sqlmock.New()
	t.Cleanup(func() { _ = platformDB.Close() })
	tenantDB, _, _ := sqlmock.New()
	t.Cleanup(func() { _ = tenantDB.Close() })
	p.pool = platformDB
	p.tenants["42"], _ = gorm.Open(mysql.New(mysql.Config{Conn: tenantDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	rp := &routingPool{owner: p}

	ctxWithRoute := func(pool *sql.DB, table string) context.Context {
		return context.WithValue(context.Background(), routeKey{}, route{p, pool, table})
	}

	t.Run("platform query returns platform pool", func(t *testing.T) {
		ctx := ctxWithRoute(platformDB, "sys_user")
		got, err := rp.selectPool(ctx, "SELECT * FROM sys_user WHERE id = ?")
		if err != nil || got != platformDB {
			t.Errorf("got %v err %v, want platformDB", got, err)
		}
	})
	t.Run("business query returns tenant pool", func(t *testing.T) {
		ctx := ctxWithRoute(tenantDB, "biz_order")
		got, err := rp.selectPool(ctx, "SELECT * FROM biz_order WHERE id = ?")
		if err != nil || got != tenantDB {
			t.Errorf("got %v err %v, want tenantDB", got, err)
		}
	})
	t.Run("missing route rejected", func(t *testing.T) {
		_, err := rp.selectPool(context.Background(), "SELECT * FROM biz_order WHERE id = ?")
		if !errors.Is(err, ErrUnsafeRoute) {
			t.Errorf("expected ErrUnsafeRoute, got %v", err)
		}
	})
	t.Run("wrong owner rejected", func(t *testing.T) {
		other := &isolation{}
		ctx := context.WithValue(context.Background(), routeKey{}, route{other, platformDB, "biz_order"})
		_, err := rp.selectPool(ctx, "SELECT * FROM biz_order WHERE id = ?")
		if !errors.Is(err, ErrUnsafeRoute) {
			t.Errorf("expected ErrUnsafeRoute, got %v", err)
		}
	})
	t.Run("unsafe SQL rejected", func(t *testing.T) {
		ctx := ctxWithRoute(tenantDB, "biz_order")
		_, err := rp.selectPool(ctx, "SELECT * FROM biz_order; DROP TABLE biz_order")
		if !errors.Is(err, ErrUnsafeRoute) {
			t.Errorf("expected ErrUnsafeRoute, got %v", err)
		}
	})
	t.Run("wrong table in SQL rejected", func(t *testing.T) {
		ctx := ctxWithRoute(tenantDB, "biz_order")
		_, err := rp.selectPool(ctx, "SELECT * FROM biz_item WHERE id = ?")
		if !errors.Is(err, ErrUnsafeRoute) {
			t.Errorf("expected ErrUnsafeRoute, got %v", err)
		}
	})
}

func TestRoutingPoolPrepareContextRejected(t *testing.T) {
	rp := &routingPool{owner: &isolation{tables: bizTables()}}
	_, err := rp.PrepareContext(context.Background(), "SELECT * FROM biz_order")
	if !errors.Is(err, ErrUnsafeRoute) {
		t.Errorf("PrepareContext should always reject, got %v", err)
	}
}

func TestRoutingTxCrossPoolRejected(t *testing.T) {
	p := &isolation{tables: bizTables(), tenants: map[string]*gorm.DB{}}
	platformDB, platformMock, _ := sqlmock.New()
	t.Cleanup(func() { _ = platformDB.Close() })
	tenantDB, _, _ := sqlmock.New()
	t.Cleanup(func() { _ = tenantDB.Close() })
	p.pool = platformDB
	p.tenants["42"], _ = gorm.Open(mysql.New(mysql.Config{Conn: tenantDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	rp := &routingPool{owner: p}

	ctx1 := context.WithValue(context.Background(), routeKey{}, route{p, platformDB, "sys_user"})
	ctx2 := context.WithValue(context.Background(), routeKey{}, route{p, tenantDB, "biz_order"})

	tx, err := rp.BeginTx(ctx1, nil)
	if err != nil {
		t.Fatal(err)
	}
	rtx := tx.(*routingTx)
	// First statement pins to platformDB.
	platformMock.ExpectBegin()
	platformMock.ExpectExec("DELETE FROM sys_user").WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := rtx.ExecContext(ctx1, "DELETE FROM sys_user WHERE id = ?", 1); err != nil {
		t.Fatalf("first statement should succeed: %v", err)
	}
	// Second statement targets tenantDB — must be rejected.
	_, err = rtx.ExecContext(ctx2, "SELECT * FROM biz_order WHERE id = ?", 1)
	if !errors.Is(err, ErrUnsafeRoute) {
		t.Errorf("cross-pool statement should be rejected, got %v", err)
	}
	_ = rtx.Rollback()
}

func TestConfigureIsolationDisabled(t *testing.T) {
	db, _ := newDB(t)
	err := configureIsolation(db, config.Database{Isolation: config.Isolation{Enabled: false}})
	if err != nil {
		t.Errorf("disabled isolation should return nil, got %v", err)
	}
}

func TestConfigureIsolationValidation(t *testing.T) {
	db, _ := newDB(t)
	base := config.Database{
		Type: "mysql", Host: "localhost", Port: 3306, User: "root", Name: "platform",
		Isolation: config.Isolation{
			Enabled: true, MaxPools: 4, MaxOpen: 10, MaxIdle: 2,
			Tables: []string{"biz_order"},
			Tenants: map[string]config.TenantDatabase{
				"1": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
			},
		},
	}

	cases := []struct {
		name   string
		mutate func(*config.Database)
	}{
		{"no tables", func(c *config.Database) { c.Isolation.Tables = nil }},
		{"no tenants", func(c *config.Database) { c.Isolation.Tenants = nil }},
		{"too many tenants", func(c *config.Database) {
			c.Isolation.MaxPools = 1
			c.Isolation.Tenants["2"] = config.TenantDatabase{}
		}},
		{"negative max open", func(c *config.Database) { c.Isolation.MaxOpen = -1 }},
		{"negative max idle", func(c *config.Database) { c.Isolation.MaxIdle = -1 }},
		{"max idle exceeds max open", func(c *config.Database) { c.Isolation.MaxIdle = 20 }},
		{"negative lifetime", func(c *config.Database) { c.Isolation.Lifetime = -1 }},
		{"invalid table name", func(c *config.Database) { c.Isolation.Tables = []string{"biz-order"} }},
		{"sys prefix business table", func(c *config.Database) { c.Isolation.Tables = []string{"sys_fake"} }},
		{"conflicting boundary", func(c *config.Database) { c.Isolation.Tables = []string{"sys_user"} }},
		{"invalid platform table", func(c *config.Database) { c.Isolation.PlatformTables = []string{"bad-name"} }},
		{"non-canonical tenant id", func(c *config.Database) {
			c.Isolation.Tenants = map[string]config.TenantDatabase{
				"01": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
			}
		}},
		{"zero tenant id", func(c *config.Database) {
			c.Isolation.Tenants = map[string]config.TenantDatabase{
				"0": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
			}
		}},
		{"negative tenant id", func(c *config.Database) {
			c.Isolation.Tenants = map[string]config.TenantDatabase{
				"-1": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
			}
		}},
		{"missing password env", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.PasswordEnv = "NEXADMIN_NONEXISTENT_ENV_VAR"
			c.Isolation.Tenants["1"] = t
		}},
		{"type mismatch", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.Type = "postgres"
			c.Isolation.Tenants["1"] = t
		}},
		{"empty host", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.Host = ""
			c.Isolation.Tenants["1"] = t
		}},
		{"invalid port", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.Port = 0
			c.Isolation.Tenants["1"] = t
		}},
		{"empty user", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.User = ""
			c.Isolation.Tenants["1"] = t
		}},
		{"invalid db name", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.Name = "bad-name"
			c.Isolation.Tenants["1"] = t
		}},
		{"empty password env", func(c *config.Database) {
			t := c.Isolation.Tenants["1"]
			t.PasswordEnv = ""
			c.Isolation.Tenants["1"] = t
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base
			c.mutate(&cfg)
			err := configureIsolation(db, cfg)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestConfigureIsolationDuplicateEndpoints(t *testing.T) {
	t.Setenv("NEXADMIN_TEST_PWD", "test")
	db, _ := newDB(t)
	cfg := config.Database{
		Type: "mysql", Host: "localhost", Port: 3306, User: "root", Name: "platform",
		Isolation: config.Isolation{
			Enabled: true, MaxPools: 4, MaxOpen: 10,
			Tables: []string{"biz_order"},
			Tenants: map[string]config.TenantDatabase{
				"1": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
				"2": {Type: "mysql", Host: "localhost", Port: 3306, User: "root", PasswordEnv: "NEXADMIN_TEST_PWD", Name: "tenant1"},
			},
		},
	}
	err := configureIsolation(db, cfg)
	if err == nil {
		t.Error("duplicate endpoint should be rejected")
	}
}

// TestIsolationRoutingIT is a real dual-database integration test.
// Gate: NEXADMIN_IT_MYSQL_DSN must be set with an empty database name.
// Example: NEXADMIN_IT_MYSQL_DSN='root:pass@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=UTC'
func TestIsolationRoutingIT(t *testing.T) {
	dsn := os.Getenv("NEXADMIN_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NEXADMIN_IT_MYSQL_DSN not set")
	}
	platformName := fmt.Sprintf("nexadmin_it_plat_%d", time.Now().UnixNano())
	tenantName := fmt.Sprintf("nexadmin_it_tenant_%d", time.Now().UnixNano())
	platCfg := liveMySQL(t, platformName)
	t.Cleanup(func() {
		drop, err := openRaw(platCfg, "", logger.Silent)
		if err != nil {
			return
		}
		defer Close(drop)
		_ = drop.Exec("DROP DATABASE IF EXISTS `" + platformName + "`").Error
		_ = drop.Exec("DROP DATABASE IF EXISTS `" + tenantName + "`").Error
	})

	if err := EnsureDatabase(context.Background(), platCfg); err != nil {
		t.Fatal(err)
	}
	tenantCfg := platCfg
	tenantCfg.Name = tenantName
	if err := EnsureDatabase(context.Background(), tenantCfg); err != nil {
		t.Fatal(err)
	}

	platformGorm, err := openRaw(platCfg, platformName, logger.Silent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Close(platformGorm) })

	// Create tables: biz_order in tenant db, sys_user in platform db.
	if err := platformGorm.Exec("CREATE TABLE `" + platformName + "`.sys_user (id BIGINT PRIMARY KEY, name VARCHAR(100))").Error; err != nil {
		t.Fatal(err)
	}
	tenantGorm, err := openRaw(tenantCfg, tenantName, logger.Silent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Close(tenantGorm) })
	if err := tenantGorm.Exec("CREATE TABLE biz_order (id BIGINT PRIMARY KEY, name VARCHAR(100), tenant_id BIGINT)").Error; err != nil {
		t.Fatal(err)
	}
	// Seed a row directly in the tenant database.
	if err := tenantGorm.Exec("INSERT INTO biz_order (id, name, tenant_id) VALUES (1, 'routed', 42)").Error; err != nil {
		t.Fatal(err)
	}

	// Configure isolation on the platform GORM connection.
	t.Setenv("NEXADMIN_IT_TENANT_PWD", platCfg.Password)
	isoCfg := config.Database{
		Type: "mysql", Host: platCfg.Host, Port: platCfg.Port, User: platCfg.User, Password: platCfg.Password, Name: platformName,
		Isolation: config.Isolation{
			Enabled: true, MaxPools: 4, MaxOpen: 5,
			Tables: []string{"biz_order"},
			Tenants: map[string]config.TenantDatabase{
				"42": {Type: "mysql", Host: platCfg.Host, Port: platCfg.Port, User: platCfg.User, PasswordEnv: "NEXADMIN_IT_TENANT_PWD", Name: tenantName},
			},
		},
	}
	if err := configureIsolation(platformGorm, isoCfg); err != nil {
		t.Fatal(err)
	}

	// Query biz_order with tenant 42 context — should route to tenant database.
	ctx := tenant.With(context.Background(), 42)
	var result struct {
		ID   int64
		Name string
	}
	if err := platformGorm.WithContext(ctx).Table("biz_order").Where("id = ?", 1).Take(&result).Error; err != nil {
		t.Fatalf("query should route to tenant db: %v", err)
	}
	if result.Name != "routed" {
		t.Errorf("expected 'routed', got %q", result.Name)
	}

	// Unconfigured tenant (99) must be rejected.
	ctx99 := tenant.With(context.Background(), 99)
	err = platformGorm.WithContext(ctx99).Table("biz_order").Where("id = ?", 1).Take(&result).Error
	if !errors.Is(err, ErrUnsafeRoute) {
		t.Errorf("unconfigured tenant should be rejected, got %v", err)
	}

	// Skipped tenant context must be rejected for business tables.
	ctxSkip := tenant.Skip(context.Background())
	err = platformGorm.WithContext(ctxSkip).Table("biz_order").Where("id = ?", 1).Take(&result).Error
	if !errors.Is(err, ErrUnsafeRoute) {
		t.Errorf("skipped tenant should be rejected for business table, got %v", err)
	}

	// Platform table writes work without tenant context, but only through GORM (raw SQL is rejected by design).
	if err := platformGorm.Table("sys_user").Create(map[string]any{"id": 1, "name": "admin"}).Error; err != nil {
		t.Fatalf("platform insert: %v", err)
	}
	var platformResult struct {
		ID   int64
		Name string
	}
	if err := platformGorm.Table("sys_user").Where("id = ?", 1).Take(&platformResult).Error; err != nil {
		t.Fatalf("platform query: %v", err)
	}
	if platformResult.Name != "admin" {
		t.Errorf("expected 'admin', got %q", platformResult.Name)
	}
}
