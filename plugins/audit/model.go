package audit

import (
	"context"
	"encoding/json"

	"nexadmin/core/auth"
	"nexadmin/core/database"
	"nexadmin/pkg/idgen"
)

// 操作类型。
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// SystemOperator 是取不到当前用户时记录的操作人名称（前端按 operatorId == "0" 显示本地化的「系统」）。
const SystemOperator = "system"

// Config 对应 plg_audit_config：每个租户一行，保存被审计的表（JSON 数组）。
type Config struct {
	database.TenantModel
	TableList string `gorm:"column:table_list;type:text;not null" json:"-"`
	UpdatedBy string `gorm:"size:100;not null" json:"updatedBy"`
}

func (Config) TableName() string { return "plg_audit_config" }

// Log 对应 plg_audit_log：一行 = 一条数据行的一次变更。
type Log struct {
	database.TenantModel
	Table        string   `gorm:"column:table_name;size:128;not null" json:"table"`
	RowPK        string   `gorm:"column:row_pk;size:255;not null" json:"rowPk"`
	Action       string   `gorm:"size:16;not null" json:"action"`
	OperatorID   idgen.ID `gorm:"not null" json:"operatorId"`
	OperatorName string   `gorm:"size:100;not null" json:"operatorName"`
	Fields       string   `gorm:"size:500;not null" json:"fields"`
	Changes      string   `gorm:"type:text;not null" json:"-"`
}

func (Log) TableName() string { return "plg_audit_log" }

// LogDetail 是详情接口的返回：变更列表 + 解析后的字段级 diff。
type LogDetail struct {
	Log
	Changes json.RawMessage `json:"changes"`
}

type operatorKey struct{}
type skipKey struct{}

// WithOperator 把当前操作人放进 context。插件自己的中间件会对其后注册的路由自动做这件事；
// 业务代码（后台任务、框架外的路由）可以手动调用，让回调能记到操作人。
func WithOperator(ctx context.Context, id int64, name string) context.Context {
	return context.WithValue(ctx, operatorKey{}, operator{id: id, name: name})
}

type operator struct {
	id   int64
	name string
}

// operatorFrom 先看显式 WithOperator，再看 core 放进 request context 的登录用户（ClaimsFromContext），
// 再回落到 auth.ContextWithUser 放入的归属用户（后台任务等无登录态场景），都没有才是系统。
func operatorFrom(ctx context.Context) (int64, string) {
	if o, ok := ctx.Value(operatorKey{}).(operator); ok && (o.id != 0 || o.name != "") {
		return o.id, o.name
	}
	if cl := auth.ClaimsFromContext(ctx); cl != nil && (cl.UserID != 0 || cl.Username != "") {
		return int64(cl.UserID), cl.Username
	}
	if id, name, ok := auth.UserFromContext(ctx); ok {
		return int64(id), name
	}
	return 0, SystemOperator
}

// markSkip 标记 context：带它的写入不再被审计（插件自己写记录/配置时使用）。
func markSkip(ctx context.Context) context.Context { return context.WithValue(ctx, skipKey{}, true) }

func skipped(ctx context.Context) bool {
	v, _ := ctx.Value(skipKey{}).(bool)
	return v
}
