// Package testkit 提供框架测试共用的最小构造器。
// 它只依赖框架已有的 memory cache，不启动数据库、不读取生产配置。
package testkit

import (
	"context"
	"testing"

	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// Context 返回带租户的测试 context。
func Context(id idgen.ID) context.Context {
	return tenant.With(context.Background(), id)
}

// PlatformContext 返回绕过租户隔离的平台级测试 context。
func PlatformContext() context.Context {
	return tenant.Skip(context.Background())
}

// MemoryCache 返回内存缓存，并在测试结束时关闭它。
func MemoryCache(t testing.TB) cache.Cache {
	t.Helper()
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
