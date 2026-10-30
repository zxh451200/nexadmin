package tenant

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/core/middleware"
	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "tenant" }

// Register 注册 /api/v1/tenants 与 /api/v1/tenant-packages。
// 租户与套餐是平台级数据，两组路由都要求超管（RequireSuper）；权限码 tenant:* 与 tenantPkg:*。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	h := &Handler{svc: NewService(d.DB)}
	ctenant.SetSource(h.svc) // 过期/额度/套餐菜单/域名绑定的强制执行信息来源

	p := r.Group("/tenant-packages", middleware.RequireSuper())
	p.GET("/options", core.Perm("tenant:list"), h.PackageOptions) // 租户表单下拉，固定路径先于 /:id
	p.GET("", core.Perm("tenantPkg:list"), h.ListPackages)
	p.GET("/:id", core.Perm("tenantPkg:list"), h.GetPackage)
	p.POST("", core.Perm("tenantPkg:create"), h.CreatePackage)
	p.PUT("/:id", core.Perm("tenantPkg:update"), h.UpdatePackage)
	p.DELETE("/:id", core.Perm("tenantPkg:delete"), h.DeletePackage)
	p.DELETE("", core.Perm("tenantPkg:delete"), h.DeletePackages)

	t := r.Group("/tenants", middleware.RequireSuper())
	t.GET("", core.Perm("tenant:list"), h.ListTenants)
	t.GET("/:id/usage", core.Perm("tenant:list"), h.Usage)
	t.GET("/:id", core.Perm("tenant:list"), h.GetTenant)
	t.POST("", core.Perm("tenant:create"), h.CreateTenant)
	t.PUT("/:id/renew", core.Perm("tenant:update"), h.Renew)
	t.PUT("/:id/status", core.Perm("tenant:update"), h.SetStatus)
	t.PUT("/:id", core.Perm("tenant:update"), h.UpdateTenant)
	t.DELETE("/:id", core.Perm("tenant:delete"), h.DeleteTenant)
	t.DELETE("", core.Perm("tenant:delete"), h.DeleteTenants)

	// 租户自助：登录即可访问，只暴露调用者自己租户的资料与用量。
	self := r.Group("/tenant-self")
	self.GET("", core.Perm("tenantSelf:view"), h.Self)
	self.PUT("", core.Perm("tenantSelf:update"), h.UpdateSelf)
}

// 种子 ID 与前端 mock 一致：套餐 7400000000010000000+序号，租户 7500000000010000000+序号。
func pkgSeedID(n int) idgen.ID    { return idgen.ID(7400000000010000000 + int64(n)) }
func tenantSeedID(n int) idgen.ID { return idgen.ID(7500000000010000000 + int64(n)) }

// 套餐的菜单 ID 与前端 mock 一致（1850000000000000001 起）。
func menuSeedIDs(count int) IDList {
	out := make(IDList, count)
	for i := range out {
		out[i] = idgen.ID(1850000000000000001 + int64(i))
	}
	return out
}

// Seed 幂等种入出厂套餐与租户：套餐按名称、租户按域名判断，已存在一律不动。
func (module) Seed(db *gorm.DB) error {
	now := time.Now().UTC()
	day := 24 * time.Hour

	pkgs := []struct {
		n            int
		name         string
		menus, users int
		storageMb    int64
		status       string
		remark       string
	}{
		{1, "体验版 Trial", 2, 5, 512, "1", "14 天试用，功能受限"},
		{2, "基础版 Basic", 3, 20, 2048, "1", "小团队入门"},
		{3, "标准版 Standard", 4, 50, 10240, "1", ""},
		{4, "专业版 Professional", 5, 200, 51200, "1", "含全部系统管理功能"},
		{5, "企业版 Enterprise", 6, 1000, 204800, "1", "含全部功能与优先支持"},
		{6, "旗舰版 Ultimate", 6, 0, 0, "1", "用户数与存储不限"},
		{7, "教育版 Education", 4, 500, 20480, "1", "面向学校与培训机构"},
		{8, "非营利 NGO", 3, 30, 5120, "1", ""},
		{9, "合作伙伴 Partner", 5, 100, 30720, "1", "渠道合作伙伴专用"},
		{10, "定制版 Custom", 6, 300, 102400, "1", "按合同单独配置"},
		{11, "旧版 Legacy 2024", 3, 10, 1024, "0", "已下架，仅存量租户使用"},
		{12, "内测版 Beta", 4, 15, 2048, "0", "内部测试"},
	}
	for _, s := range pkgs {
		var cnt int64
		if err := db.Model(&Package{}).Where("name = ?", s.name).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		roles := 0 // 角色上限与前端 mock 一致：用户上限的 1/5（至少 3），用户不限的套餐角色也不限
		if s.users > 0 {
			roles = max(3, (s.users+4)/5)
		}
		p := &Package{Name: s.name, MenuIDs: menuSeedIDs(s.menus), MaxUsers: s.users, MaxRoles: roles, MaxStorageMb: s.storageMb,
			Status: s.status, Remark: s.remark}
		p.ID = pkgSeedID(s.n)
		p.CreatedAt = now.Add(-time.Duration(200-s.n*12) * day)
		p.UpdatedAt = p.CreatedAt
		if err := db.Create(p).Error; err != nil {
			return err
		}
	}

	tenants := []struct {
		n          int
		name, host string
		pkg        int
		expireDays int
		status     string
		admin      string
	}{
		{1, "星河科技 Galaxy Tech", "galaxy.nexadmin.dev", 5, 300, StatusActive, "galaxy_admin"},
		{2, "云帆网络", "yunfan.nexadmin.dev", 4, 180, StatusActive, "yunfan"},
		{3, "Acme Corporation", "acme.nexadmin.dev", 6, 540, StatusActive, "acme_admin"},
		{4, "北辰教育", "beichen.nexadmin.dev", 7, 90, StatusActive, "beichen_root"},
		{5, "Blue Harbor Ltd.", "blueharbor.nexadmin.dev", 3, 45, StatusActive, "bh_admin"},
		{6, "澄明医疗", "chengming.nexadmin.dev", 4, 20, StatusActive, "cm_admin"},
		{7, "Northwind Traders", "northwind.nexadmin.dev", 2, -12, StatusExpired, "nw_admin"},
		{8, "远山设计", "yuanshan.nexadmin.dev", 2, -40, StatusExpired, "ys_admin"},
		{9, "Globex Inc.", "globex.nexadmin.dev", 5, 210, StatusSuspended, "globex_admin"},
		{10, "拾光公益", "shiguang.nexadmin.dev", 8, 365, StatusActive, "sg_admin"},
		{11, "Initech", "initech.nexadmin.dev", 1, 7, StatusActive, "initech_admin"},
		{12, "启明物流", "qiming.nexadmin.dev", 9, 120, StatusSuspended, "qm_admin"},
		{13, "Umbrella Labs", "umbrella.nexadmin.dev", 10, 700, StatusActive, "umbrella_admin"},
	}
	for _, s := range tenants {
		var cnt int64
		if err := db.Model(&Tenant{}).Where("domain = ?", s.host).Count(&cnt).Error; err != nil {
			return err
		}
		if cnt > 0 {
			continue
		}
		t := &Tenant{Name: s.name, Domain: s.host, PackageID: pkgSeedID(s.pkg), ExpireAt: now.Add(time.Duration(s.expireDays) * day),
			Status: s.status, AdminUsername: s.admin}
		t.ID = tenantSeedID(s.n)
		t.CreatedAt = now.Add(time.Duration(-300+s.n*15) * day)
		t.UpdatedAt = t.CreatedAt
		if err := db.Create(t).Error; err != nil {
			return err
		}
	}
	return nil
}
