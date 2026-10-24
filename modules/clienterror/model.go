// Package clienterror 收集前端错误上报：POST /api/v1/client-errors（登录与未登录均可，按 IP 限流、请求体 <= 8KB），
// 同一租户下相同指纹的错误聚合成一行并累计次数；管理端 GET /monitor/client-errors 查看。保留 14 天，后台每小时清理。
package clienterror

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/database"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// ClientError 对应 sys_client_error：(tenant_id, fingerprint) 唯一，hit_count 为累计次数。
type ClientError struct {
	database.TenantModel
	UserID      idgen.ID  `gorm:"not null;default:0" json:"userId"`
	Username    string    `gorm:"size:64" json:"username"`
	Route       string    `gorm:"size:255" json:"route"`
	Message     string    `gorm:"size:500;not null" json:"message"`
	Stack       string    `gorm:"type:text" json:"stack"`
	UA          string    `gorm:"column:ua;size:255" json:"ua"`
	Version     string    `gorm:"size:32" json:"version"`
	Fingerprint string    `gorm:"size:64;not null" json:"fingerprint"`
	HitCount    int       `gorm:"column:hit_count;not null;default:1" json:"hitCount"`
	LastSeenAt  time.Time `gorm:"not null" json:"lastSeenAt"`
}

func (ClientError) TableName() string { return "sys_client_error" }

// Store 是落库接口；单测里用内存实现替换。
type Store interface {
	// Upsert 写入一次出现：指纹已存在则次数 +1 并刷新最近信息，否则新增。租户取自 ctx。
	Upsert(ctx context.Context, e *Event) error
	// Purge 删除最近一次出现早于 before 的记录（跨租户），返回删除行数。
	Purge(ctx context.Context, before time.Time) (int64, error)
}

type dbStore struct{ db *gorm.DB }

func (s dbStore) Upsert(ctx context.Context, e *Event) error {
	row := ClientError{
		UserID: e.UserID, Username: e.Username, Route: e.Route, Message: e.Message, Stack: e.Stack,
		UA: e.UA, Version: e.Version, Fingerprint: e.Fingerprint, HitCount: 1, LastSeenAt: e.At,
	}
	// 新行的 ID / TenantID 由 BeforeCreate 填充。冲突时累计次数，并用最新一次的路由、用户、UA、版本覆盖；
	// 消息与堆栈保留第一次的（同指纹本来就一致）。表名限定写法在 MySQL / Postgres 下都合法，避免列名歧义。
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "fingerprint"}},
		DoUpdates: clause.Assignments(map[string]any{
			"hit_count":    gorm.Expr("sys_client_error.hit_count + 1"),
			"last_seen_at": e.At,
			"updated_at":   e.At,
			"route":        e.Route,
			"user_id":      e.UserID,
			"username":     e.Username,
			"ua":           e.UA,
			"version":      e.Version,
		}),
	}).Create(&row).Error
}

func (s dbStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res := s.db.WithContext(tenant.Skip(ctx)).Where("last_seen_at < ?", before).Delete(&ClientError{})
	return res.RowsAffected, res.Error
}
