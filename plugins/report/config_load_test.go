package report

import (
	"os"
	"path/filepath"
	"testing"

	"nexadmin/core/config"
)

// config.yaml 的 report 段：能追加业务表，但拒绝列表命中的表/列会被丢弃；max_rows / query_timeout 只能往小调。
func TestLoadRegistryFromConfigFile(t *testing.T) {
	yml := `
report:
  max_rows: 999999
  query_timeout: 1h
  sources:
    - table: sys_user
      tenant_column: tenant_id
      columns: [{name: username, type: string}]
    - table: biz_order
      label: 订单
      tenant_column: tenant_id
      owner_column: created_by
      columns:
        - {name: status, label: 状态, type: string}
        - {name: amount, label: 金额, type: number}
        - {name: password_hash, type: string}
    - table: biz_invoice
      label: 发票
      tenant_column: tenant_id
      columns:
        - {name: title, type: string}
        - {name: total, type: number}
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, set := loadRegistry(&config.Config{UserConfigPath: path}, nil)
	if _, ok := reg.Get("sys_user"); ok {
		t.Fatal("sys_user must never be loadable")
	}
	if _, ok := reg.Get("biz_order"); ok {
		t.Fatal("source with a password_hash column must be dropped as a whole")
	}
	inv, ok := reg.Get("biz_invoice")
	if !ok || inv.TenantCol != "tenant_id" || len(inv.Cols) != 2 {
		t.Fatalf("valid configured source missing: %+v", inv)
	}
	if _, ok := reg.Get("plg_cron_job"); !ok {
		t.Fatal("default sources should remain")
	}
	if set.MaxRows != MaxLimit || set.Timeout != maxTimeout {
		t.Fatalf("settings not clamped: %+v", set)
	}

	// use_defaults: false -> 只有配置里的表。
	if err := os.WriteFile(path, []byte("report:\n  use_defaults: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, _ = loadRegistry(&config.Config{UserConfigPath: path}, nil)
	if len(reg.List()) != 0 {
		t.Fatalf("defaults should be disabled, got %d sources", len(reg.List()))
	}
	// 配置文件缺失 / 损坏 -> 回落到默认白名单，不 panic。
	reg, _ = loadRegistry(&config.Config{UserConfigPath: filepath.Join(t.TempDir(), "none.yaml")}, nil)
	if len(reg.List()) != len(defaultRaw()) {
		t.Fatalf("missing file should use defaults, got %d", len(reg.List()))
	}
	if err := os.WriteFile(path, []byte("report: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg, _ = loadRegistry(&config.Config{UserConfigPath: path}, nil)
	if len(reg.List()) != len(defaultRaw()) {
		t.Fatalf("broken file should use defaults, got %d", len(reg.List()))
	}
}
