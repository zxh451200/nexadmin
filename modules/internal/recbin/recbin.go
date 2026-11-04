// Package recbin 是回收站的共享约定：软删除时登记一条 sys_recycle，recycle 模块据此展示、还原、彻底删除。
// user / notice / role / dict / post 等模块在软删除时调用 Add（放在同一事务里），不必依赖 recycle 模块本身。
package recbin

import (
	"context"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 已接入回收站的类型。
const (
	TypeUser     = "user"
	TypeNotice   = "notice"
	TypeRole     = "role"
	TypeDictType = "dictType"
	TypeDictData = "dictData"
	TypePost     = "post"
)

// Tables 是类型到源表的映射；源表必须有 deleted_at 列（软删除）。
var Tables = map[string]string{
	TypeUser:     "sys_user",
	TypeNotice:   "sys_notice",
	TypeRole:     "sys_role",
	TypeDictType: "sys_dict_type",
	TypeDictData: "sys_dict_data",
	TypePost:     "sys_post",
}

// Platform 是源表没有 tenant_id 列的类型（基座 database.PlatformModel，如字典这类平台级表）。
// 回收记录本身仍然带 tenant_id（谁删的就进谁的回收站），但还原 / 彻底删除源行时不能拿它当谓词——
// 源表没有这一列，带上它 SQL 直接报错。
var Platform = map[string]bool{
	TypeDictType: true,
	TypeDictData: true,
}

// Keys 是源行的业务唯一键列（还原前据此检查撞码）。
//
// 为什么要在应用层查：这些表原本的数据库唯一索引（uk_sys_dict_type_code、
// uk_sys_dict_data_type_value、uk_sys_post_tenant_code）在接软删除时降级成了普通索引——
// 三种引擎都拿不到「只对未删行唯一」的干净写法（PG 的部分索引 MySQL 没有，跨引擎结构闸会红），
// 对齐 sys_user 的既有做法。唯一性因此改由源模块的查重逻辑保证，还原时数据库不会再抛重复键，
// 必须在这里显式比对同键的未删行，否则还原会造出两条同码数据（见 recycle.restore_conflict）。
//
// Keys 只能表达「一组列同时相等」这一条 AND 复合键。user 不适用它：账号有两把各自独立的
// 唯一键（用户名 / 手机号），撞任意一把都算冲突，是 OR 关系；而且手机号可空，空号不参与查重
// （否则无手机号的用户之间会互相误判成撞码）。这两种形状 Keys 都写不出来，
// 所以 user 的撞码判定单独放在 recycle.keyUserConflict（同租户内 username 相同，或 phone 非空且相同）。
// notice 未登记：它的唯一性仍由数据库层索引决定，撞码时由还原 UPDATE 抛重复键兜底。
var Keys = map[string][]string{
	TypeRole:     {"tenant_id", "code"},
	TypePost:     {"tenant_id", "code"},
	TypeDictType: {"code"},
	TypeDictData: {"type_id", "dict_value"},
}

// Child 是彻底删除时要一并物理清掉的关联表。
type Child struct {
	Table  string // 关联表名（常量白名单，不来自请求）
	Column string // 指向源行的列
}

// Children 登记各类型的关联表：软删除时必须原样保留（否则还原出来的就是空壳），
// 只有彻底删除才物理清掉，避免残留指向已不存在之行的关联。
var Children = map[string][]Child{
	TypeNotice: {
		{Table: "sys_notice_target", Column: "notice_id"},
		{Table: "sys_notice_read", Column: "notice_id"},
	},
	TypeRole: {
		{Table: "sys_role_menu", Column: "role_id"},
		{Table: "sys_role_dept", Column: "role_id"},
		{Table: "sys_user_role", Column: "role_id"},
	},
	TypePost: {
		{Table: "sys_user_post", Column: "post_id"},
	},
	TypeDictType: {
		{Table: "sys_dict_data", Column: "type_id"},
	},
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
// 登记行的 tenant_id 由 GORM 的租户 scope 从 ctx 填入：平台级表（Platform）的源行没有租户，
// 但回收记录仍然属于「执行删除的那个租户」，回收站列表按它隔离。
func Add(ctx context.Context, db *gorm.DB, typ string, refID idgen.ID, name, deletedBy string) error {
	e := &Entry{Type: typ, RefID: refID, Name: name, DeletedBy: deletedBy, DeletedAt: time.Now().UTC()}
	return db.WithContext(ctx).Create(e).Error
}

// Operator 取当前请求里“删掉这条数据的人”的标识，供 Add 的 deletedBy 使用。
// 优先用户 ID（回收站列表会把它翻译成昵称），其次令牌里的用户名；后台任务等没有登录态时回空串。
func Operator(ctx context.Context) string {
	if cl := auth.ClaimsFromContext(ctx); cl != nil {
		if cl.UserID != 0 {
			return cl.UserID.String()
		}
		return cl.Username
	}
	if id, _, ok := auth.UserFromContext(ctx); ok {
		return id.String()
	}
	return ""
}
