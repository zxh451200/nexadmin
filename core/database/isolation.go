package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gormmysql "gorm.io/driver/mysql"
	gormpg "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"nexadmin/core/config"
	"nexadmin/core/tenant"
)

var ErrUnsafeRoute = errors.New("database: unsafe isolated route (raw SQL, unknown table, cross-database operation or unscoped tenant access)")

// 框架控制面统一保留平台库，包括带 tenant_id 的认证、组织和运营数据。
const platformTables = `sys_user sys_user_role sys_user_post sys_role sys_role_menu sys_role_dept
sys_dept sys_post sys_menu sys_dict_type sys_dict_data sys_config sys_setting
sys_tenant sys_tenant_pkg sys_user_identity sys_pat sys_openapi_app sys_sso_app sys_sso_refresh
sys_file sys_file_group sys_notice sys_notice_target sys_notice_read sys_message sys_send_log
sys_oper_log sys_login_log sys_client_error sys_recycle sys_job sys_dashboard_layout
sys_backup sys_backup_setting sys_webhook sys_webhook_delivery sys_migration sys_plugin_migration
sys_tenant_usage_daily sys_language sys_monitor_snapshot sys_notice_clear sys_notice_dismiss
sys_region_snapshot`

type isolation struct {
	base    *gorm.DB
	pool    *sql.DB
	tenants map[string]*gorm.DB
	tables  map[string]bool // true: tenant business table
}

func (*isolation) Name() string { return "nexadmin:isolation" }

type routeKey struct{}
type route struct {
	owner *isolation
	pool  *sql.DB
	table string
}

func (p *isolation) Initialize(db *gorm.DB) error {
	callback := func(tx *gorm.DB) {
		if tx.Error != nil {
			return
		}
		defer func() {
			if tx.Error != nil {
				if rt, ok := tx.Statement.ConnPool.(*routingTx); ok {
					rt.mu.Lock()
					rt.failed = tx.Error
					rt.mu.Unlock()
				}
			}
		}()
		s := tx.Statement
		// Table expressions, joins and raw SQL cannot prove a single database boundary.
		if s.Table == "" || s.TableExpr != nil && s.TableExpr.SQL != "`"+s.Table+"`" && s.TableExpr.SQL != `"`+s.Table+`"` ||
			s.SQL.Len() != 0 {
			tx.AddError(ErrUnsafeRoute)
			return
		}
		business, known := p.tables[s.Table]
		if !known {
			tx.AddError(fmt.Errorf("%w: table %s", ErrUnsafeRoute, s.Table))
			return
		}
		target := p.pool
		if business {
			id, ok := tenant.From(s.Context)
			if !ok || id <= 0 || tenant.Skipped(s.Context) {
				tx.AddError(ErrUnsafeRoute)
				return
			}
			tdb := p.tenants[strconv.FormatInt(int64(id), 10)]
			if tdb == nil {
				tx.AddError(fmt.Errorf("%w: tenant %s has no configured pool", ErrUnsafeRoute, id))
				return
			}
			target, _ = tdb.DB()
		}
		s.Context = context.WithValue(s.Context, routeKey{}, route{p, target, s.Table})
	}
	for _, register := range []func() error{
		func() error { return db.Callback().Create().Before("*").Register("nexadmin:route", callback) },
		func() error { return db.Callback().Query().Before("*").Register("nexadmin:route", callback) },
		func() error { return db.Callback().Update().Before("*").Register("nexadmin:route", callback) },
		func() error { return db.Callback().Delete().Before("*").Register("nexadmin:route", callback) },
		func() error { return db.Callback().Row().Before("*").Register("nexadmin:route", callback) },
		func() error { return db.Callback().Raw().Before("*").Register("nexadmin:route", callback) },
	} {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

// Platform 返回仅供迁移/运维使用的未路由平台连接，不能作为业务请求的 DB。
func Platform(db *gorm.DB) *gorm.DB {
	if p, ok := db.Config.Plugins["nexadmin:isolation"].(*isolation); ok {
		return p.base
	}
	return db
}

// TenantDatabases 返回服务器显式配置的业务库；调用方不得关闭这些共享连接。
func TenantDatabases(db *gorm.DB) map[string]*gorm.DB {
	out := map[string]*gorm.DB{}
	if p, ok := db.Config.Plugins["nexadmin:isolation"].(*isolation); ok {
		for id, td := range p.tenants {
			out[id] = td
		}
	}
	return out
}

func configureIsolation(db *gorm.DB, cfg config.Database) error {
	c := cfg.Isolation
	if !c.Enabled {
		return nil
	}
	if c.MaxPools == 0 {
		c.MaxPools = 16
	}
	if c.MaxOpen == 0 {
		c.MaxOpen = 10
	}
	if c.Lifetime == 0 {
		c.Lifetime = 30 * time.Minute
	}
	if c.MaxPools < 1 || len(c.Tenants) == 0 || len(c.Tenants) > c.MaxPools ||
		c.MaxOpen < 1 || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen || c.Lifetime < 0 || len(c.Tables) == 0 {
		return errors.New("database: invalid isolation pool limits, tables or tenants")
	}
	p := &isolation{base: db.Session(&gorm.Session{NewDB: true}), tenants: map[string]*gorm.DB{}, tables: map[string]bool{}}
	p.pool, _ = db.DB()
	for _, table := range strings.Fields(platformTables) {
		p.tables[table] = false
	}
	for _, table := range c.PlatformTables {
		if !identRe.MatchString(table) {
			return errors.New("database: invalid platform table")
		}
		p.tables[table] = false
	}
	for _, table := range c.Tables {
		if !identRe.MatchString(table) || strings.HasPrefix(table, "sys_") {
			return errors.New("database: invalid business table (sys_ is reserved)")
		}
		if _, exists := p.tables[table]; exists {
			return errors.New("database: conflicting table boundary")
		}
		p.tables[table] = true
	}
	// 独立 GORM callback registry，共用同一平台 sql.DB；迁移不经过请求路由。
	var dial gorm.Dialector
	if db.Dialector.Name() == "mysql" {
		dial = gormmysql.New(gormmysql.Config{Conn: p.pool, SkipInitializeWithVersion: true})
	} else {
		dial = gormpg.New(gormpg.Config{Conn: p.pool})
	}
	var err error
	p.base, err = gorm.Open(dial, &gorm.Config{Logger: db.Logger, NowFunc: db.NowFunc, DisableAutomaticPing: true})
	if err != nil {
		return err
	}
	if err := p.base.Use(tenantPlugin{}); err != nil {
		return err
	}
	seen := map[string]bool{fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Name): true}
	ids := make([]string, 0, len(c.Tenants))
	for id := range c.Tenants {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fail := func(err error) error {
		for _, td := range p.tenants {
			_ = Close(td)
		}
		return err
	}
	for _, id := range ids {
		t := c.Tenants[id]
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
			return fail(errors.New("database: tenant keys must be canonical positive decimal IDs"))
		}
		kind, err := Normalize(t.Type)
		baseKind, _ := Normalize(cfg.Type)
		if err != nil || kind != baseKind || t.Host == "" || t.Port < 1 || t.Port > 65535 || t.User == "" || !identRe.MatchString(t.Name) || t.PasswordEnv == "" {
			return fail(fmt.Errorf("database: invalid connection configuration for tenant %s", id))
		}
		key := fmt.Sprintf("%s:%d/%s", t.Host, t.Port, t.Name)
		if seen[key] {
			return fail(errors.New("database: tenant databases must have distinct endpoints and names"))
		}
		seen[key] = true
		password, ok := os.LookupEnv(t.PasswordEnv)
		if !ok || password == "" {
			return fail(fmt.Errorf("database: password environment variable missing for tenant %s", id))
		}
		td, err := openRaw(config.Database{Type: t.Type, Host: t.Host, Port: t.Port, User: t.User, Password: password, Name: t.Name}, t.Name, logger.Silent)
		if err != nil {
			return fail(fmt.Errorf("database: cannot open configured pool for tenant %s", id))
		}
		p.tenants[id] = td
		sp, _ := td.DB()
		sp.SetMaxOpenConns(c.MaxOpen)
		sp.SetMaxIdleConns(c.MaxIdle)
		sp.SetConnMaxLifetime(c.Lifetime)
		if err := td.Use(tenantPlugin{}); err != nil {
			return fail(err)
		}
	}
	if err := db.Use(p); err != nil {
		return fail(err)
	}
	r := &routingPool{owner: p}
	db.Config.ConnPool = r
	db.Statement.ConnPool = r
	db.Config.DisableNestedTransaction = false
	return nil
}

// routingPool 不实现 GetDBConn，防止 db.DB()/Connection 绕过路由。
type routingPool struct{ owner *isolation }

func (p *routingPool) selectPool(ctx context.Context, query string) (*sql.DB, error) {
	r, ok := ctx.Value(routeKey{}).(route)
	if !ok || r.owner != p.owner {
		return nil, ErrUnsafeRoute
	}
	if !singleTableSQL(query, r.table, p.owner.tables) {
		return nil, ErrUnsafeRoute
	}
	return r.pool, nil
}

// 保守词法白名单：仅单表 DML，拒绝 SQL 注释、限定库名、子查询、CTE、
// JOIN、多语句以及 MySQL 多表 UPDATE/DELETE。不解析字符串中的 SQL 关键字。
func singleTableSQL(q, table string, tables map[string]bool) bool {
	var tokens []string
	for i := 0; i < len(q); {
		c := q[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if c == ';' || c == '#' || c == '\\' || c == '/' || c == '-' && i+1 < len(q) && q[i+1] == '-' {
			return false
		}
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			start := i
			var value strings.Builder
			closed := false
			for i < len(q) {
				if q[i] == '\\' {
					return false
				}
				if q[i] == quote {
					value.WriteString(q[start:i])
					i++
					if i < len(q) && q[i] == quote {
						value.WriteByte(quote)
						i++
						start = i
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return false
			}
			if quote == '\'' {
				tokens = append(tokens, "?")
			} else {
				tokens = append(tokens, strings.ToLower(value.String()))
			}
			continue
		}
		start := i
		for i < len(q) && (q[i] >= 'a' && q[i] <= 'z' || q[i] >= 'A' && q[i] <= 'Z' || q[i] >= '0' && q[i] <= '9' || q[i] == '_' || q[i] == '$') {
			i++
		}
		if start == i {
			tokens = append(tokens, string(c))
			i++
		} else {
			tokens = append(tokens, strings.ToLower(q[start:i]))
		}
	}
	if len(tokens) < 3 {
		return false
	}
	op := tokens[0]
	if op != "select" && op != "insert" && op != "update" && op != "delete" {
		return false
	}
	target := -1
	fromCount := 0
	for i, t := range tokens {
		switch t {
		case "select":
			if i != 0 {
				return false
			}
		case "union", "with", "intersect", "except", "using", "call", "copy", "load", "outfile", "dumpfile":
			return false
		case "join":
			// Only joins wholly inside the declared boundary are allowed.
			if op != "select" || i+1 >= len(tokens) {
				return false
			}
			b, ok := tables[tokens[i+1]]
			if !ok || b != tables[table] || i+2 < len(tokens) && tokens[i+2] == "." {
				return false
			}
		case "from":
			fromCount++
			if fromCount > 1 || op != "select" && op != "delete" {
				return false
			}
			target = i + 1
		case "into":
			if op != "insert" || i != 1 {
				return false
			}
			target = i + 1
		case "update":
			// MySQL ON DUPLICATE KEY UPDATE is safe; other embedded UPDATE is not.
			if i == 0 {
				target = 1
			} else if i < 3 || tokens[i-1] != "key" || tokens[i-2] != "duplicate" {
				return false
			}
		}
	}
	if target < 1 || target >= len(tokens) || tokens[target] != strings.ToLower(table) {
		return false
	}
	if target+1 < len(tokens) && tokens[target+1] == "." {
		return false
	}
	if op == "select" {
		depth := 0
		for i := target + 1; i < len(tokens); i++ {
			t := tokens[i]
			if depth == 0 && (t == "where" || t == "group" || t == "order" || t == "limit" || t == "having") {
				break
			}
			if t == "(" {
				depth++
			}
			if t == ")" {
				depth--
			}
			if depth == 0 && t == "," {
				return false
			}
		}
		return true
	}
	// No comma table lists or aliases: GORM's normal single-table output has a clause immediately after its table.
	if target+1 < len(tokens) {
		next := tokens[target+1]
		switch op {
		case "select", "delete":
			switch next {
			case "where", "order", "group", "having", "limit", "offset", "returning", "for":
			default:
				return false
			}
		case "update":
			if next != "set" {
				return false
			}
		case "insert":
			if next != "(" && next != "values" && next != "default" {
				return false
			}
		}
	}
	return true
}

func (p *routingPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, ErrUnsafeRoute
}
func (p *routingPool) ExecContext(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	db, err := p.selectPool(ctx, q)
	if err != nil {
		return nil, err
	}
	return db.ExecContext(ctx, q, args...)
}
func (p *routingPool) QueryContext(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	db, err := p.selectPool(ctx, q)
	if err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, q, args...)
}

// GORM 的 QueryRow 接口没有 error 返回值，用关闭的 sql.DB 生成可 Scan 的错误行。
var rejectedRows = func() *sql.DB { db, _ := sql.Open("mysql", ""); _ = db.Close(); return db }()

func (p *routingPool) QueryRowContext(ctx context.Context, q string, args ...interface{}) *sql.Row {
	db, err := p.selectPool(ctx, q)
	if err != nil {
		return rejectedRows.QueryRowContext(ctx, q, args...)
	}
	return db.QueryRowContext(ctx, q, args...)
}
func (p *routingPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	return &routingTx{routingPool: p, ctx: ctx, opts: opts}, nil
}

// 首条语句懒绑定；后续每条语句重新校验目标，跨库和跨租户事务均拒绝。
type routingTx struct {
	*routingPool
	mu     sync.Mutex
	ctx    context.Context
	opts   *sql.TxOptions
	tx     *sql.Tx
	pinned *sql.DB
	done   bool
	failed error
}

func (t *routingTx) bind(ctx context.Context, q string) (*sql.Tx, error) {
	if t.done {
		return nil, sql.ErrTxDone
	}
	if t.failed != nil {
		return nil, t.failed
	}
	db, err := t.selectPool(ctx, q)
	if err == nil && t.pinned != nil && db != t.pinned {
		err = ErrUnsafeRoute
	}
	if err != nil {
		t.failed = err
		return nil, err
	}
	if t.tx == nil {
		t.tx, err = db.BeginTx(t.ctx, t.opts)
		if err != nil {
			t.failed = err
			return nil, err
		}
		t.pinned = db
	}
	return t.tx, nil
}
func (t *routingTx) ExecContext(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tx, err := t.bind(ctx, q)
	if err != nil {
		return nil, err
	}
	return tx.ExecContext(ctx, q, args...)
}
func (t *routingTx) QueryContext(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tx, err := t.bind(ctx, q)
	if err != nil {
		return nil, err
	}
	return tx.QueryContext(ctx, q, args...)
}
func (t *routingTx) QueryRowContext(ctx context.Context, q string, args ...interface{}) *sql.Row {
	t.mu.Lock()
	defer t.mu.Unlock()
	tx, err := t.bind(ctx, q)
	if err != nil {
		return rejectedRows.QueryRowContext(ctx, q, args...)
	}
	return tx.QueryRowContext(ctx, q, args...)
}
func (t *routingTx) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	if t.failed != nil {
		if t.tx != nil {
			_ = t.tx.Rollback()
		}
		return t.failed
	}
	if t.tx == nil {
		return nil
	}
	return t.tx.Commit()
}
func (t *routingTx) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	if t.tx == nil {
		return nil
	}
	return t.tx.Rollback()
}
