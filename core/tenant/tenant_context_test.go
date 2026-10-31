package tenant_test

import (
	"context"
	"testing"

	"nexadmin/core/tenant"
)

// Unskip 是「Skip 全局扫描 → 按实例切回租户」的关键一步：With 不会覆盖 skip 标记。
func TestUnskipRestoresIsolation(t *testing.T) {
	ctx := tenant.With(tenant.Unskip(tenant.Skip(context.Background())), 42)
	if tenant.Skipped(ctx) {
		t.Fatal("Unskip 后仍处于跳过隔离状态")
	}
	if id, ok := tenant.From(ctx); !ok || id != 42 {
		t.Fatalf("tenant.From = %d,%v, want 42,true", id, ok)
	}
}
