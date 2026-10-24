// Package tenant 是租户与租户套餐模块（平台级表，仅超管可管理）。
package tenant

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 租户状态。
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusExpired   = "expired"
)

// IDList 是 ID 数组，库里存 JSON 文本，对外序列化为字符串数组。
type IDList []idgen.ID

// Value 实现 driver.Valuer。
func (l IDList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal(l)
	return string(b), err
}

// Scan 实现 sql.Scanner。
func (l *IDList) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*l = IDList{}
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("tenant: cannot scan %T into IDList", src)
	}
	if len(raw) == 0 {
		*l = IDList{}
		return nil
	}
	return json.Unmarshal(raw, l)
}

// GormDataType 告诉 GORM 列类型为 TEXT。
func (IDList) GormDataType() string { return "text" }

// Package 对应表 sys_tenant_pkg，JSON 字段与前端 TenantPackageRow 一致。
type Package struct {
	database.PlatformModel
	Name         string `gorm:"size:100;not null" json:"name"`
	MenuIDs      IDList `gorm:"column:menu_ids;not null" json:"menuIds"`
	MaxUsers     int    `gorm:"not null" json:"maxUsers"`      // 0 不限制
	MaxRoles     int    `gorm:"not null" json:"maxRoles"`      // 0 不限制
	MaxStorageMb int64  `gorm:"not null" json:"maxStorageMb"`  // MB，0 不限制
	AiDailyQuota int    `gorm:"not null" json:"aiDailyQuota"`  // AI 每日调用次数，0 不限制
	Status       string `gorm:"size:1;not null" json:"status"` // "1" 启用 "0" 停用
	Remark       string `gorm:"size:500;not null" json:"remark,omitempty"`
}

// TableName 指定表名。
func (Package) TableName() string { return "sys_tenant_pkg" }

// Tenant 对应表 sys_tenant，JSON 字段与前端 TenantRow 一致。
type Tenant struct {
	database.PlatformModel
	Name          string    `gorm:"size:100;not null" json:"name"`
	Domain        string    `gorm:"size:255;not null" json:"domain"`
	PackageID     idgen.ID  `gorm:"not null" json:"packageId"`
	ExpireAt      time.Time `gorm:"not null" json:"expireAt"`
	Status        string    `gorm:"size:16;not null" json:"status"`
	AdminUsername string    `gorm:"size:64;not null" json:"adminUsername"`
	// 租户级额度覆盖：nil = 沿用套餐，0 = 不限制。
	MaxUsers     *int   `json:"maxUsers"`
	MaxRoles     *int   `json:"maxRoles"`
	MaxStorageMb *int64 `json:"maxStorageMb"`
	// AI：AiEnabled=false 时该租户不能用 AI；AiDailyQuota 是每日调用次数覆盖（nil = 沿用套餐，0 = 不限制）。
	AiEnabled    bool `gorm:"not null" json:"aiEnabled"`
	AiDailyQuota *int `json:"aiDailyQuota"`
}

// TableName 指定表名。
func (Tenant) TableName() string { return "sys_tenant" }

// PackageInput 是套餐新增/修改的请求体（前端 TenantPackageInput）。
type PackageInput struct {
	Name         string     `json:"name" binding:"required,max=100"`
	MenuIDs      []idgen.ID `json:"menuIds"`
	MaxUsers     int        `json:"maxUsers" binding:"min=0"`
	MaxRoles     int        `json:"maxRoles" binding:"min=0"`
	MaxStorageMb int64      `json:"maxStorageMb" binding:"min=0"`
	AiDailyQuota int        `json:"aiDailyQuota" binding:"min=0"`
	Status       string     `json:"status" binding:"omitempty,oneof=0 1"`
	Remark       string     `json:"remark" binding:"max=500"`
}

// TenantInput 是租户新增/修改的请求体（前端 TenantInput）。
// expireAt 接受 yyyy-MM-dd（取当天正午 UTC，避免时区造成日期偏移）或 RFC3339。
// createAdmin=true 且新建时，会在同一事务里创建租户管理员账号并赋予「租户管理员」角色（adminUsername / adminPassword / adminEmail）；
// 初始密码只用于生成哈希，不存储、不回显、不写日志。maxUsers / maxRoles / maxStorageMb 为空表示沿用套餐，0 表示不限制。
type TenantInput struct {
	Name          string   `json:"name" binding:"required,max=100"`
	Domain        string   `json:"domain" binding:"required,max=255"`
	PackageID     idgen.ID `json:"packageId" binding:"required"`
	ExpireAt      string   `json:"expireAt" binding:"required"`
	Status        string   `json:"status" binding:"omitempty,oneof=active suspended expired"`
	AdminUsername string   `json:"adminUsername" binding:"max=64"`
	AdminPassword string   `json:"adminPassword" binding:"max=72"`
	AdminEmail    string   `json:"adminEmail" binding:"max=100"`
	CreateAdmin   bool     `json:"createAdmin"`
	MaxUsers      *int     `json:"maxUsers" binding:"omitempty,min=0"`
	MaxRoles      *int     `json:"maxRoles" binding:"omitempty,min=0"`
	MaxStorageMb  *int64   `json:"maxStorageMb" binding:"omitempty,min=0"`
	// AiEnabled 为空表示保持现状（新建时默认开启）；AiDailyQuota 为空表示沿用套餐，0 表示不限制。
	AiEnabled    *bool `json:"aiEnabled"`
	AiDailyQuota *int  `json:"aiDailyQuota" binding:"omitempty,min=0"`
}

// ListQuery 是 GET /tenants 与 GET /tenant-packages 的查询参数。
type ListQuery struct {
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	PackageID string `form:"packageId"`
}

// Option 是 GET /tenant-packages/options 的返回项。
type Option struct {
	ID   idgen.ID `json:"id"`
	Name string   `json:"name"`
}

// Usage 是 GET /tenants/:id/usage 的返回。max* 为有效额度（租户覆盖优先，其次套餐），0 表示不限制。
type Usage struct {
	Users        int64     `json:"users"`
	MaxUsers     int       `json:"maxUsers"`
	StorageMB    int64     `json:"storageMB"`
	MaxStorageMB int64     `json:"maxStorageMB"`
	Roles        int64     `json:"roles"`
	MaxRoles     int       `json:"maxRoles"`
	ExpireAt     time.Time `json:"expireAt"`
	DaysLeft     int       `json:"daysLeft"`
}
