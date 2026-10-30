package tenant_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/migrate"
	ctenant "nexadmin/core/tenant"
	"nexadmin/modules/system/tenant"
	"nexadmin/pkg/idgen"
)

// liveConfig 从环境变量读取真实库连接；未设置 NEXADMIN_LIVE_DB 时跳过。
// 例：NEXADMIN_LIVE_DB=mysql NEXADMIN_LIVE_PORT=3306 NEXADMIN_LIVE_USER=root NEXADMIN_LIVE_PASSWORD=... NEXADMIN_LIVE_DB_NAME=nexadmin_tenant_live
func liveConfig(t *testing.T) (config.Database, bool) {
	t.Helper()
	kind := os.Getenv("NEXADMIN_LIVE_DB")
	if kind == "" {
		t.Skip("未设置 NEXADMIN_LIVE_DB，跳过真实库验证")
	}
	port, _ := strconv.Atoi(os.Getenv("NEXADMIN_LIVE_PORT"))
	host := os.Getenv("NEXADMIN_LIVE_HOST")
	if host == "" {
		host = "localhost"
	}
	name := os.Getenv("NEXADMIN_LIVE_DB_NAME")
	if name == "" {
		name = "nexadmin_tenant_live"
	}
	return config.Database{Type: kind, Host: host, Port: port, User: os.Getenv("NEXADMIN_LIVE_USER"),
		Password: os.Getenv("NEXADMIN_LIVE_PASSWORD"), Name: name, NodeID: 9}, true
}

// TestLiveTenantProvisioningAndSelf 在真实 MySQL / PostgreSQL 上验证：
// 新增迁移可执行、开通初始化落库、我的租户读写与用量切片幂等。
func TestLiveTenantProvisioningAndSelf(t *testing.T) {
	cfg, ok := liveConfig(t)
	if !ok {
		return
	}
	ctx := context.Background()
	if err := idgen.Init(9); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureDatabase(ctx, cfg); err != nil {
		t.Fatalf("ensure database: %v", err)
	}
	db, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 可选清理：仅当显式设置 NEXADMIN_LIVE_CLEANUP=1 且库名以 _live 结尾时删库，避免误删。
	if os.Getenv("NEXADMIN_LIVE_CLEANUP") == "1" && strings.HasSuffix(cfg.Name, "_live") {
		t.Cleanup(func() {
			_ = database.Close(db)
			// PostgreSQL 不能在连着目标库的会话里删它，先切到 postgres 维护库。
			adminCfg := cfg
			if cfg.Type == "postgres" {
				adminCfg.Name = "postgres"
			}
			admin, err := database.Open(adminCfg)
			if err != nil {
				t.Logf("cleanup open: %v", err)
				return
			}
			stmt := "DROP DATABASE `" + cfg.Name + "`"
			if cfg.Type == "postgres" {
				stmt = `DROP DATABASE "` + cfg.Name + `"`
			}
			if err := admin.Exec(stmt).Error; err != nil {
				t.Logf("cleanup drop: %v", err)
			}
			_ = database.Close(admin)
		})
	}
	sources, err := migrate.DefaultSources()
	if err != nil {
		t.Fatal(err)
	}
	st, err := migrate.NewGormStore(database.Platform(db))
	if err != nil {
		t.Fatal(err)
	}
	res, err := migrate.NewRunner(st, zap.NewNop().Sugar(), sources...).Up()
	if err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	t.Logf("migrated %d files", len(res.Applied))

	// 出厂菜单由应用启动时的 Seed 写入，migrate 只建表；这里补三条平台级菜单，
	// 否则开通初始化按权限码查不到菜单（真实安装不会缺）。
	now := time.Now().UTC()
	for _, perm := range []string{"dashboard:view", "profile:view", "about:view"} {
		var n int64
		if err := db.WithContext(ctenant.Skip(ctx)).Table("sys_menu").Where("perm = ?", perm).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			continue
		}
		mid, err := idgen.Next()
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO sys_menu (id, parent_id, code, type, title, icon, path, component, perm, sort, keep_alive, hidden, external, status, created_at, updated_at)
			VALUES (?, 0, ?, 'menu', ?, '', '/x', '', ?, 10, TRUE, FALSE, FALSE, '1', ?, ?)`, int64(mid), perm, perm, perm, now, now).Error; err != nil {
			t.Fatalf("seed menu %s: %v", perm, err)
		}
	}

	svc := tenant.NewService(db)
	ctenant.SetSource(svc)
	sid, err := idgen.Next()
	if err != nil {
		t.Fatal(err)
	}
	suffix := sid.String()

	pkg := &tenant.Package{Name: "live-pkg-" + suffix, MaxUsers: 5, MaxRoles: 3, MaxStorageMb: 100}
	if err := db.Create(pkg).Error; err != nil {
		t.Fatal(err)
	}
	in := tenant.TenantInput{
		Name: "live-tenant-" + suffix, Domain: "live-" + suffix + ".nexadmin.dev", PackageID: pkg.ID,
		ExpireAt: "2027-01-01", Status: tenant.StatusActive, CreateAdmin: true,
		AdminUsername: "live_admin_" + suffix, AdminPassword: "Live-Pass-123",
		CompanyName: "企业甲", ContactEmail: "a@example.com", LogoURL: "/uploads/a.png",
	}
	tr, err := svc.CreateTenant(ctx, in)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	sctx := ctenant.Skip(ctx)
	count := func(table, where string, args ...any) int64 {
		var n int64
		if err := db.WithContext(sctx).Table(table).Where(where, args...).Count(&n).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	if got := count("sys_dept", "tenant_id = ?", int64(tr.ID)); got != 1 {
		t.Errorf("root departments = %d, want 1", got)
	}
	if got := count("sys_post", "tenant_id = ? AND code = ?", int64(tr.ID), "default"); got != 1 {
		t.Errorf("default posts = %d, want 1", got)
	}
	if got := count("sys_role", "tenant_id = ? AND code = ? AND builtin = ?", int64(tr.ID), "staff", false); got != 1 {
		t.Errorf("staff roles = %d, want 1 (and must not be builtin, builtin means all permissions)", got)
	}
	var staffMenus int64
	if err := db.WithContext(sctx).Table("sys_role_menu").Select("COUNT(*)").
		Joins("JOIN sys_role r ON r.id = sys_role_menu.role_id").
		Where("r.tenant_id = ? AND r.code = ?", int64(tr.ID), "staff").Scan(&staffMenus).Error; err != nil {
		t.Fatalf("count staff menus: %v", err)
	}
	if staffMenus == 0 {
		t.Errorf("staff role has no granted menus")
	}

	tctx := ctenant.With(ctx, tr.ID)
	self, err := svc.Self(tctx)
	if err != nil {
		t.Fatalf("self: %v", err)
	}
	if self.PackageName != pkg.Name || self.LogoURL != "/uploads/a.png" || len(self.Trend) != 1 {
		t.Errorf("self view = package %q logo %q trend %d", self.PackageName, self.LogoURL, len(self.Trend))
	}
	if _, err := svc.UpdateSelf(tctx, tenant.SelfInput{LogoURL: "javascript:alert(1)"}); err == nil {
		t.Error("javascript: logo must be rejected")
	}
	if _, err := svc.UpdateSelf(tctx, tenant.SelfInput{CompanyName: "企业乙", ContactEmail: "b@example.com", LogoURL: "/uploads/b.png"}); err != nil {
		t.Fatalf("update self: %v", err)
	}
	// 平台端整单修改租户，不得抹掉租户自助维护的品牌字段。
	if _, err := svc.UpdateTenant(ctx, tr.ID, tenant.TenantInput{Name: in.Name, Domain: in.Domain, PackageID: pkg.ID,
		ExpireAt: "2028-01-01", Status: tenant.StatusActive, AdminUsername: in.AdminUsername}); err != nil {
		t.Fatalf("platform update: %v", err)
	}
	var after tenant.Tenant
	if err := db.Where("id = ?", int64(tr.ID)).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.LogoURL != "/uploads/b.png" || after.CompanyName != "企业乙" {
		t.Errorf("platform update wiped branding: logo=%q company=%q", after.LogoURL, after.CompanyName)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.Self(tctx); err != nil {
			t.Fatalf("self again: %v", err)
		}
	}
	if got := count("sys_tenant_usage_daily", "tenant_id = ?", int64(tr.ID)); got != 1 {
		t.Errorf("usage daily rows = %d, want 1 (upsert must be idempotent)", got)
	}
	fmt.Println("live tenant verification done on", cfg.Type)
}
