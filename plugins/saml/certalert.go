package saml

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/cache"
	"nexadmin/core/i18n"
	"nexadmin/core/notify"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type setNXCacher interface {
	SetNX(context.Context, string, string, time.Duration) (bool, error)
}

type certAlert struct {
	SourceID idgen.ID `gorm:"column:source_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
	Name     string   `gorm:"column:name"`
	Cert     string   `gorm:"column:sp_cert"`
	NextCert string   `gorm:"column:next_sp_cert"`
}

type certAlertHolder struct {
	UserID   idgen.ID `gorm:"column:user_id"`
	TenantID idgen.ID `gorm:"column:tenant_id"`
}

func sweepCertAlerts(ctx context.Context, db *gorm.DB, sender *notify.Service, c cache.Cache, log *zap.Logger, now time.Time) {
	if db == nil || sender == nil || c == nil {
		return
	}
	var sources []certAlert
	err := db.WithContext(tenant.Skip(ctx)).Table("plg_saml_source").
		Select("id AS source_id, tenant_id, name, sp_cert, next_sp_cert").
		Where("enabled = ?", true).Find(&sources).Error
	if err != nil {
		if log != nil {
			log.Warn("saml: certificate alert query failed", zap.Error(err))
		}
		return
	}
	nx, ok := c.(setNXCacher)
	if !ok {
		return
	}
	for _, source := range sources {
		for _, item := range []struct {
			role string
			cert string
		}{{"current", source.Cert}, {"next", source.NextCert}} {
			expires, err := spCertNotAfter(item.cert)
			if err != nil || expires.After(now.Add(certWarnWindow)) {
				continue
			}
			days := int(expires.Sub(now).Hours() / 24)
			if expires.Before(now) {
				days = 0
			}
			key := cache.GlobalKey("saml-cert-alert", source.TenantID.String(), source.SourceID.String(), item.role, now.UTC().Format("2006-01-02"))
			claimed, err := nx.SetNX(tenant.Skip(ctx), key, "1", 25*time.Hour)
			if err != nil {
				if log != nil {
					log.Warn("saml: certificate alert dedup failed", zap.Error(err))
				}
				continue
			}
			if !claimed {
				continue
			}
			sendCertAlert(ctx, db, sender, source, item.role, expires, days, log)
		}
	}
}

func sendCertAlert(ctx context.Context, db *gorm.DB, sender *notify.Service, source certAlert, role string, expires time.Time, days int, log *zap.Logger) {
	holders := db.WithContext(tenant.Skip(ctx)).Table("sys_user_role AS ur").Select("ur.user_id").
		Joins("JOIN sys_role r ON r.id = ur.role_id AND r.status = ?", "1").
		Joins("LEFT JOIN sys_role_menu rm ON rm.role_id = r.id").
		Joins("LEFT JOIN sys_menu m ON m.id = rm.menu_id AND m.status = ? AND m.perm = ?", "1", "saml:edit").
		Where("r.builtin = ? OR m.id IS NOT NULL", true)
	var users []certAlertHolder
	if err := db.WithContext(tenant.Skip(ctx)).Table("sys_user AS u").
		Select("DISTINCT u.id AS user_id, u.tenant_id AS tenant_id").
		Joins("JOIN (?) AS permitted ON permitted.user_id = u.id", holders).
		Where("u.tenant_id = ? AND u.deleted_at IS NULL AND u.status = ?", source.TenantID, "1").Scan(&users).Error; err != nil {
		if log != nil {
			log.Warn("saml: certificate alert recipient query failed", zap.Error(err))
		}
		return
	}
	ids := make([]idgen.ID, 0, len(users))
	for _, u := range users {
		if u.TenantID == source.TenantID {
			ids = append(ids, u.UserID)
		}
	}
	if len(ids) == 0 {
		return
	}
	tctx := tenant.With(ctx, source.TenantID)
	roleText := i18n.T(tctx, "saml.cert_alert.current")
	if role == "next" {
		roleText = i18n.T(tctx, "saml.cert_alert.next")
	}
	msg := notify.Msg{
		To: ids, Category: "saml", Channels: []string{notify.ChannelInApp},
		Title: i18n.T(tctx, "saml.cert_alert.title"),
		Content: i18n.T(tctx, "saml.cert_alert.content", source.Name, roleText,
			expires.UTC().Format("2006-01-02"), days),
		Link: "/system/saml",
	}
	if err := sender.Send(tctx, msg); err != nil && log != nil {
		log.Warn("saml: certificate alert send failed", zap.Int64("tenant", int64(source.TenantID)), zap.Error(err))
	}
}
