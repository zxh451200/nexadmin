// Package recbin 是回收站的共享约定：软删除时登记一条 sys_recycle，recycle 模块据此展示、还原、彻底删除。
// user / notice 等模块在软删除时调用 Add（放在同一事务里），不必依赖 recycle 模块本身。
package recbin

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 已接入回收站的类型。
const (
	TypeUser   = "user"
	TypeNotice = "notice"
)

// Tables 是类型到源表的映射；源表必须有 deleted_at 列（软删除）。
var Tables = map[string]string{
	TypeUser:   "sys_user",
	TypeNotice: "sys_notice",
}

// Entry 对应 sys_recycle：只记录“谁在什么时候删了哪一行”，数据本身留在源表里。
type Entry struct {
	database.TenantModel
	Type      string   `gorm:"size:32;not null;index"`
	RefID     idgen.ID `gorm:"not null"`
	Name      string   `gorm:"size:200"`
	DeletedBy string   `gorm:"size:64"`
	DeletedAt time.Time
}

// TableName 固定表名。
func (Entry) TableName() string { return "sys_recycle" }

// Add 登记一条回收记录。name 是展示名（用户昵称、公告标题等）。
func Add(ctx context.Context, db *gorm.DB, typ string, refID idgen.ID, name, deletedBy string) error {
	e := &Entry{Type: typ, RefID: refID, Name: name, DeletedBy: deletedBy, DeletedAt: time.Now().UTC()}
	return db.WithContext(ctx).Create(e).Error
}
