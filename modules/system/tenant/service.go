package tenant

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Service 封装租户与套餐的数据访问与业务规则。
type Service struct{ db *gorm.DB }

// NewService 创建服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func bad(key string, args ...any) error      { return response.BadRequest(key, args...) }
func conflict(key string, args ...any) error { return response.Conflict(key, args...) }
func notFound() error                        { return response.NotFound("error.not_found") }

func statusOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func like(kw string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(strings.TrimSpace(kw))) + "%"
}

func toInt64(ids []idgen.ID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// parseExpire 解析到期时间：yyyy-MM-dd 取当天正午 UTC，否则按 RFC3339。
func parseExpire(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Add(12 * time.Hour), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, bad("tenant.expire_invalid")
	}
	return t.UTC(), nil
}

// ---- 套餐 ----

// ListPackages 分页查询套餐。
func (s *Service) ListPackages(ctx context.Context, q ListQuery, page, size int) ([]Package, int64, error) {
	db := s.db.WithContext(ctx).Model(&Package{})
	if strings.TrimSpace(q.Keyword) != "" {
		db = db.Where("LOWER(name) LIKE ? OR LOWER(remark) LIKE ?", like(q.Keyword), like(q.Keyword))
	}
	if q.Status != "" {
		db = db.Where("status IN ?", strings.Split(q.Status, ","))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Package{}
	err := db.Order("created_at, id").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

// PackageOptions 返回启用套餐的 id+name，供租户表单下拉（可按名称搜索）。
func (s *Service) PackageOptions(ctx context.Context, keyword string) ([]Option, error) {
	db := s.db.WithContext(ctx).Where("status = ?", "1")
	if strings.TrimSpace(keyword) != "" {
		db = db.Where("LOWER(name) LIKE ?", like(keyword))
	}
	var rows []Package
	if err := db.Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Option, 0, len(rows))
	for _, r := range rows {
		out = append(out, Option{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// GetPackage 读取单个套餐。
func (s *Service) GetPackage(ctx context.Context, id idgen.ID) (*Package, error) {
	var p Package
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &p, nil
}

// CreatePackage 新增套餐。
func (s *Service) CreatePackage(ctx context.Context, in PackageInput) (*Package, error) {
	p := &Package{Name: in.Name, MenuIDs: IDList(in.MenuIDs), MaxUsers: in.MaxUsers, MaxRoles: in.MaxRoles, MaxStorageMb: in.MaxStorageMb,
		AiDailyQuota: in.AiDailyQuota, Status: statusOr(in.Status, "1"), Remark: in.Remark}
	if p.MenuIDs == nil {
		p.MenuIDs = IDList{}
	}
	return p, s.db.WithContext(ctx).Create(p).Error
}

// UpdatePackage 修改套餐。
func (s *Service) UpdatePackage(ctx context.Context, id idgen.ID, in PackageInput) (*Package, error) {
	if _, err := s.GetPackage(ctx, id); err != nil {
		return nil, err
	}
	menus := IDList(in.MenuIDs)
	if menus == nil {
		menus = IDList{}
	}
	err := s.db.WithContext(ctx).Model(&Package{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "menu_ids": menus, "max_users": in.MaxUsers, "max_roles": in.MaxRoles, "max_storage_mb": in.MaxStorageMb,
		"ai_daily_quota": in.AiDailyQuota, "status": statusOr(in.Status, "1"), "remark": in.Remark,
	}).Error
	if err != nil {
		return nil, err
	}
	ctenant.InvalidateAll() // 套餐额度与菜单影响该套餐下所有租户（并通知其它实例）
	return s.GetPackage(ctx, id)
}

// DeletePackages 批量删除套餐；仍被租户使用的套餐不允许删除。
func (s *Service) DeletePackages(ctx context.Context, ids []idgen.ID) error {
	raw := toInt64(ids)
	var used int64
	if err := s.db.WithContext(ctx).Model(&Tenant{}).Where("package_id IN ?", raw).Count(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return conflict("tenant.package_in_use")
	}
	return s.db.WithContext(ctx).Where("id IN ?", raw).Delete(&Package{}).Error
}

// ---- 租户 ----

// ListTenants 分页查询租户。
func (s *Service) ListTenants(ctx context.Context, q ListQuery, page, size int) ([]Tenant, int64, error) {
	db := s.db.WithContext(ctx).Model(&Tenant{})
	if strings.TrimSpace(q.Keyword) != "" {
		l := like(q.Keyword)
		db = db.Where("LOWER(name) LIKE ? OR LOWER(domain) LIKE ? OR LOWER(admin_username) LIKE ?", l, l, l)
	}
	if q.Status != "" {
		db = db.Where("status IN ?", strings.Split(q.Status, ","))
	}
	if q.PackageID != "" {
		pid, err := idgen.Parse(q.PackageID)
		if err != nil {
			return nil, 0, response.BadRequest("error.bad_request").Wrap(err)
		}
		db = db.Where("package_id = ?", int64(pid))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	list := []Tenant{}
	err := db.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

// GetTenant 读取单个租户。
func (s *Service) GetTenant(ctx context.Context, id idgen.ID) (*Tenant, error) {
	var t Tenant
	if err := s.db.WithContext(ctx).Where("id = ?", int64(id)).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, notFound()
		}
		return nil, err
	}
	return &t, nil
}

// check 校验套餐存在，规整并校验域名（大小写不敏感、格式、保留域名），并确保域名未被其他租户占用。
func (s *Service) check(ctx context.Context, in *TenantInput, exclude idgen.ID) error {
	if _, err := s.GetPackage(ctx, in.PackageID); err != nil {
		return bad("tenant.package_not_found")
	}
	d, err := NormalizeDomain(in.Domain)
	if err != nil {
		return err
	}
	in.Domain = d
	var n int64
	err = s.db.WithContext(ctx).Model(&Tenant{}).Where("LOWER(domain) = ? AND id <> ?", d, int64(exclude)).Count(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return response.Conflict("tenant.domain_taken")
	}
	return nil
}

// CreateTenant 新增租户。createAdmin=true 时在同一事务里创建租户管理员，任一步失败整体回滚。
func (s *Service) CreateTenant(ctx context.Context, in TenantInput) (*Tenant, error) {
	exp, err := parseExpire(in.ExpireAt)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, &in, 0); err != nil {
		return nil, err
	}
	var plan *adminPlan
	if in.CreateAdmin {
		if plan, err = newAdminPlan(in); err != nil {
			return nil, err
		}
		in.AdminUsername = plan.username
	} else {
		in.AdminUsername = strings.TrimSpace(in.AdminUsername)
	}
	t := &Tenant{Name: in.Name, Domain: in.Domain, PackageID: in.PackageID, ExpireAt: exp,
		Status: statusOr(in.Status, StatusActive), AdminUsername: in.AdminUsername,
		MaxUsers: in.MaxUsers, MaxRoles: in.MaxRoles, MaxStorageMb: in.MaxStorageMb,
		AiEnabled: in.AiEnabled == nil || *in.AiEnabled, AiDailyQuota: in.AiDailyQuota}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		if plan == nil {
			return nil
		}
		return createTenantAdmin(ctx, tx, t.ID, plan)
	})
	if err != nil {
		return nil, err
	}
	ctenant.Invalidate(t.ID)
	return t, nil
}

// UpdateTenant 修改租户。
func (s *Service) UpdateTenant(ctx context.Context, id idgen.ID, in TenantInput) (*Tenant, error) {
	cur, err := s.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	exp, err := parseExpire(in.ExpireAt)
	if err != nil {
		return nil, err
	}
	if err := s.check(ctx, &in, id); err != nil {
		return nil, err
	}
	admin := strings.TrimSpace(in.AdminUsername)
	if admin == "" {
		admin = cur.AdminUsername
	}
	aiEnabled := cur.AiEnabled // 请求没带 aiEnabled 时保持现状，旧客户端的整单提交不会意外改动开关
	if in.AiEnabled != nil {
		aiEnabled = *in.AiEnabled
	}
	err = s.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"name": in.Name, "domain": in.Domain, "package_id": int64(in.PackageID), "expire_at": exp,
		"status": statusOr(in.Status, cur.Status), "admin_username": admin,
		"max_users": in.MaxUsers, "max_roles": in.MaxRoles, "max_storage_mb": in.MaxStorageMb,
		"ai_enabled": aiEnabled, "ai_daily_quota": in.AiDailyQuota,
	}).Error
	if err != nil {
		return nil, err
	}
	ctenant.Invalidate(id)
	return s.GetTenant(ctx, id)
}

// Renew 续期；已过期的租户续期后恢复为正常。
func (s *Service) Renew(ctx context.Context, id idgen.ID, expireAt string) (*Tenant, error) {
	t, err := s.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	exp, err := parseExpire(expireAt)
	if err != nil {
		return nil, err
	}
	status := t.Status
	if status == StatusExpired {
		status = StatusActive
	}
	err = s.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", int64(id)).
		Updates(map[string]any{"expire_at": exp, "status": status}).Error
	if err != nil {
		return nil, err
	}
	ctenant.Invalidate(id)
	return s.GetTenant(ctx, id)
}

// SetStatus 启停租户。
func (s *Service) SetStatus(ctx context.Context, id idgen.ID, status string) (*Tenant, error) {
	if _, err := s.GetTenant(ctx, id); err != nil {
		return nil, err
	}
	if status != StatusActive && status != StatusSuspended && status != StatusExpired {
		return nil, bad("tenant.status_invalid")
	}
	if err := s.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", int64(id)).Update("status", status).Error; err != nil {
		return nil, err
	}
	ctenant.Invalidate(id)
	return s.GetTenant(ctx, id)
}

// DeleteTenants 批量删除租户记录（不会清理该租户下的业务数据）。
func (s *Service) DeleteTenants(ctx context.Context, ids []idgen.ID) error {
	err := s.db.WithContext(ctx).Where("id IN ?", toInt64(ids)).Delete(&Tenant{}).Error
	ctenant.Invalidate(ids...)
	return err
}
