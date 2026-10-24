package database

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// ErrNoTenant 表示访问租户表时 context 里既没有租户，也没有显式跳过隔离。
// 宁可报错也不放行：这是“租户不串”的兜底。
var ErrNoTenant = errors.New("database: no tenant in context (use tenant.Skip(ctx) for platform-level access)")

// PlatformModel 是平台级表（菜单/字典/参数/权限标识）的基座：雪花主键，不进租户 scope。
type PlatformModel struct {
	ID        idgen.ID  `gorm:"primaryKey;autoIncrement:false" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// BeforeCreate 在 ID 为空时由应用侧生成雪花 ID。
func (m *PlatformModel) BeforeCreate(_ *gorm.DB) error {
	if m.ID != 0 {
		return nil
	}
	id, err := idgen.Next()
	if err != nil {
		return err
	}
	m.ID = id
	return nil
}

// TenantModel 是租户表的基座：业务模型嵌入它，查询/更新/删除自动追加 tenant_id 条件，
// 创建时自动填入当前租户。业务表不得使用 sys_ 前缀。
type TenantModel struct {
	PlatformModel
	TenantID idgen.ID `gorm:"index;not null" json:"tenantId"`
}

// BeforeCreate 生成 ID 并填入当前租户。
func (m *TenantModel) BeforeCreate(tx *gorm.DB) error {
	if err := m.PlatformModel.BeforeCreate(tx); err != nil {
		return err
	}
	if m.TenantID != 0 {
		return nil
	}
	ctx := tx.Statement.Context
	if tenant.Skipped(ctx) {
		return nil
	}
	id, ok := tenant.From(ctx)
	if !ok {
		return ErrNoTenant
	}
	m.TenantID = id
	return nil
}

// tenantPlugin 给带 tenant_id 列的模型的查询/更新/删除自动追加 WHERE tenant_id = ?。
// 原生 SQL（db.Raw / db.Exec）不经过这里，需要自己带条件。
type tenantPlugin struct{}

func (tenantPlugin) Name() string { return "nexadmin:tenant" }

func (tenantPlugin) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	if err := cb.Query().Before("gorm:query").Register("nexadmin:tenant_query", tenantScope); err != nil {
		return err
	}
	if err := cb.Row().Before("gorm:row").Register("nexadmin:tenant_row", tenantScope); err != nil {
		return err
	}
	if err := cb.Update().Before("gorm:update").Register("nexadmin:tenant_update", tenantScope); err != nil {
		return err
	}
	return cb.Delete().Before("gorm:delete").Register("nexadmin:tenant_delete", tenantScope)
}

func tenantScope(db *gorm.DB) {
	stmt := db.Statement
	if db.Error != nil || stmt.Schema == nil {
		return
	}
	field := stmt.Schema.LookUpField("tenant_id")
	if field == nil {
		return
	}
	if tenant.Skipped(stmt.Context) {
		return
	}
	id, ok := tenant.From(stmt.Context)
	if !ok {
		_ = db.AddError(ErrNoTenant)
		return
	}
	stmt.AddClause(clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: field.DBName}, Value: int64(id)},
	}})
}
