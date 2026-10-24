package config

import "testing"

func TestValidateTrustedProxies(t *testing.T) {
	for _, ok := range [][]string{nil, {}, {"127.0.0.1"}, {"10.0.0.0/8", "::1", "fd00::/8"}, {" 192.168.1.1 "}} {
		if err := ValidateTrustedProxies(ok); err != nil {
			t.Errorf("%v should be valid: %v", ok, err)
		}
	}
	for _, bad := range [][]string{{""}, {"nginx"}, {"10.0.0.0/33"}, {"127.0.0.1", "not-an-ip"}} {
		if err := ValidateTrustedProxies(bad); err == nil {
			t.Errorf("%v should be rejected", bad)
		}
	}
}

// 默认空（不信任任何代理）；环境变量可用逗号分隔列表覆盖；非法值启动即报错。
func TestLoadTrustedProxies(t *testing.T) {
	def := "../../config/default.yaml"
	none := t.TempDir() + "/none.yaml"
	cfg, err := Load(def, none)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Server.TrustedProxies) != 0 {
		t.Fatalf("default must trust no proxy, got %v", cfg.Server.TrustedProxies)
	}
	t.Setenv("NEXADMIN_SERVER_TRUSTED_PROXIES", "127.0.0.1,10.0.0.0/8")
	cfg, err = Load(def, none)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Server.TrustedProxies; len(got) != 2 || got[0] != "127.0.0.1" || got[1] != "10.0.0.0/8" {
		t.Fatalf("env override = %v", got)
	}
	t.Setenv("NEXADMIN_SERVER_TRUSTED_PROXIES", "not-an-ip")
	if _, err := Load(def, none); err == nil {
		t.Fatal("invalid entry must fail validation")
	}
}
