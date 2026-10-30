package tenant

import (
	"context"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"nexadmin/core/response"
	ctenant "nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

const (
	trendDays = 30
	dayLayout = "2006-01-02"
)

// Self 返回调用者所在租户的资料、有效额度/用量与近 30 天用量切片。
// 平台账号（超管）和不受管的默认租户没有租户记录，返回 404。
func (s *Service) Self(ctx context.Context) (*Self, error) {
	id, ok := ctenant.From(ctx)
	if !ok {
		return nil, notFound()
	}
	t, err := s.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}
	info, err := s.TenantInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, notFound()
	}
	usage, err := s.Usage(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.snapshot(ctx, id, usage); err != nil {
		return nil, err
	}
	trend, err := s.trend(ctx, id)
	if err != nil {
		return nil, err
	}
	name := ""
	if t.PackageID != 0 {
		if p, err := s.GetPackage(ctx, t.PackageID); err == nil {
			name = p.Name
		}
	}
	days, _ := info.DaysLeft(time.Now())
	return &Self{
		Name: t.Name, Domain: t.Domain, Status: t.Status, PackageName: name,
		ExpireAt: t.ExpireAt, DaysLeft: days,
		CompanyName: t.CompanyName, ContactName: t.ContactName, ContactPhone: t.ContactPhone,
		ContactEmail: t.ContactEmail, LogoURL: t.LogoURL,
		AiEnabled: t.AiEnabled, AiDailyQuota: info.AIDailyQuota,
		Usage: *usage, Trend: trend,
	}, nil
}

// UpdateSelf 只允许租户修改企业资料、联系人与 logo；名称/域名/套餐/额度/到期归平台管理。
func (s *Service) UpdateSelf(ctx context.Context, in SelfInput) (*Self, error) {
	id, ok := ctenant.From(ctx)
	if !ok {
		return nil, notFound()
	}
	if _, err := s.GetTenant(ctx, id); err != nil {
		return nil, err
	}
	if err := validateTenantProfile(in.CompanyName, in.ContactEmail, in.LogoURL); err != nil {
		return nil, err
	}
	email := strings.TrimSpace(in.ContactEmail)
	logo := strings.TrimSpace(in.LogoURL)
	err := s.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", int64(id)).Updates(map[string]any{
		"company_name": strings.TrimSpace(in.CompanyName), "contact_name": strings.TrimSpace(in.ContactName),
		"contact_phone": strings.TrimSpace(in.ContactPhone), "contact_email": email, "logo_url": logo,
	}).Error
	if err != nil {
		return nil, err
	}
	ctenant.Invalidate(id)
	return s.Self(ctx)
}

// validateTenantProfile 校验租户可维护的企业资料字段（新建与自助修改共用）。
func validateTenantProfile(_ string, email, logo string) error {
	email = strings.TrimSpace(email)
	if email != "" && (len(email) > 200 || !adminEmailRe.MatchString(email)) {
		return response.BadRequest("tenant.contact_email_invalid")
	}
	if !validLogoURL(strings.TrimSpace(logo)) {
		return response.BadRequest("tenant.logo_invalid")
	}
	return nil
}

// validLogoURL 只接受站内绝对路径或 http(s) 绝对地址，拒绝协议相对与 data:/javascript:。
func validLogoURL(raw string) bool {
	if raw == "" {
		return true
	}
	if strings.HasPrefix(raw, "//") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "":
		return strings.HasPrefix(raw, "/")
	case "http", "https":
		return u.Host != ""
	default:
		return false
	}
}

// snapshot 幂等写入当天用量切片；唯一键 (tenant_id, day) 保证并发重复读也只留一行。
func (s *Service) snapshot(ctx context.Context, id idgen.ID, u *Usage) error {
	row := &UsageDaily{
		TenantID: id, Day: time.Now().UTC().Format(dayLayout),
		Users: u.Users, Roles: u.Roles, StorageMB: u.StorageMB,
	}
	return s.db.WithContext(ctenant.Skip(ctx)).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "day"}},
			DoUpdates: clause.AssignmentColumns([]string{"users", "roles", "storage_mb", "updated_at"}),
		}).
		Create(row).Error
}

func (s *Service) trend(ctx context.Context, id idgen.ID) ([]Point, error) {
	from := time.Now().UTC().AddDate(0, 0, -trendDays+1).Format(dayLayout)
	rows := []UsageDaily{}
	err := s.db.WithContext(ctenant.Skip(ctx)).
		Where("tenant_id = ? AND day >= ?", int64(id), from).
		Order("day ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]Point, 0, len(rows))
	for _, r := range rows {
		out = append(out, Point{Day: r.Day, Users: r.Users, Roles: r.Roles, StorageMB: r.StorageMB})
	}
	return out, nil
}
