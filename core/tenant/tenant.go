// Package tenant 定义租户 context 约定：解析结果注入 context，tenant.From(ctx) 读取。
package tenant

import (
	"context"

	"github.com/gin-gonic/gin"

	"nexadmin/pkg/idgen"
)

// DefaultID 是解析不到任何租户时使用的默认租户。
const DefaultID idgen.ID = 1

type idKey struct{}
type skipKey struct{}

// With 把租户 ID 写入 context。
func With(ctx context.Context, id idgen.ID) context.Context {
	return context.WithValue(ctx, idKey{}, id)
}

// From 读取当前租户。
func From(ctx context.Context) (idgen.ID, bool) {
	id, ok := ctx.Value(idKey{}).(idgen.ID)
	return id, ok
}

// Skip 返回一个跳过租户隔离的 context（超管端 /api/v1/admin、初始化、种子数据使用）。
func Skip(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipKey{}, true)
}

// Skipped 报告当前 context 是否跳过租户隔离。
func Skipped(ctx context.Context) bool {
	v, _ := ctx.Value(skipKey{}).(bool)
	return v
}

// Resolver 从请求里解析租户；解析不到返回 false，由框架回退到下一级。
// 用 core.WithTenantResolver 可整体替换默认解析链（子域名 > X-Tenant-ID > JWT claim > 默认租户）。
type Resolver func(c *gin.Context) (idgen.ID, bool)

// DomainLookup 把请求域名映射到租户（租户模块的“域名绑定”实现它）；未提供则跳过子域名这一级。
type DomainLookup func(host string) (idgen.ID, bool)
