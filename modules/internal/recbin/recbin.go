// Package recbin 已迁移到 nexadmin/pkg/recbin——CRM 等插件（plugins/*）也要接回收站，
// internal 路径挡住了它们，所以真身搬到 pkg/。这里保留类型与映射的别名壳过渡：
// modules/* 的既有 import 不用动；新代码（插件侧）一律直接用 nexadmin/pkg/recbin。
package recbin

import (
	"context"

	"gorm.io/gorm"

	pkgrecbin "nexadmin/pkg/recbin"
	"nexadmin/pkg/idgen"
)

// 类型常量与映射全部别名到 pkg/recbin 的同一实例：这里改不了内容，改了也不会被 recycle 看到。
const (
	TypeUser     = pkgrecbin.TypeUser
	TypeNotice   = pkgrecbin.TypeNotice
	TypeRole     = pkgrecbin.TypeRole
	TypeDictType = pkgrecbin.TypeDictType
	TypeDictData = pkgrecbin.TypeDictData
	TypePost     = pkgrecbin.TypePost
	// TypeCustomer 由插件侧直接用 pkgrecbin.TypeCustomer，这里不别名——壳只服务 modules/* 既有代码。
)

var (
	Tables   = pkgrecbin.Tables
	Keys     = pkgrecbin.Keys
	Children = pkgrecbin.Children
	Platform = pkgrecbin.Platform
)

// Entry 类型别名：recycle 读写 sys_recycle 的行类型与插件写入的是同一个 Go 类型。
type Entry = pkgrecbin.Entry

// Add / Operator 是函数，无法别名，包一层转发。
func Add(ctx context.Context, db *gorm.DB, typ string, refID idgen.ID, name, deletedBy string) error {
	return pkgrecbin.Add(ctx, db, typ, refID, name, deletedBy)
}

func Operator(ctx context.Context) string { return pkgrecbin.Operator(ctx) }
