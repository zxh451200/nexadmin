package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProviderConfExpandsEnvAndHidesIncomplete(t *testing.T) {
	t.Setenv("T_GOOGLE_SECRET", "s3cret")
	a := Auth{Providers: map[string]any{
		"google": map[string]any{
			"client_id": "cid", "client_secret": "${T_GOOGLE_SECRET}", "redirect_uris": []any{" https://a/cb "},
			"match_by": " Email ", "auto_create": true, "allowed_email_domains": []any{"@Corp.com", " "},
		},
		"github":   map[string]any{"client_id": "x", "client_secret": "${T_UNSET_VAR}", "redirect_uris": []any{"https://a/cb"}}, // 环境变量没设：视为未配置
		"feishu":   map[string]any{"client_id": "x", "client_secret": "y"},                                                      // 没回调白名单
		"wecom":    map[string]any{"client_id": "x", "client_secret": "y", "redirect_uris": []any{"https://a/cb"}},              // 缺 agent_id
		"oidc":     map[string]any{"client_id": "x", "client_secret": "y", "redirect_uris": []any{"https://a/cb"}},              // 缺 issuer
		"dingtalk": "not-a-map",
	}}
	c, ok := a.ProviderConf("google")
	if !ok || c.ClientSecret != "s3cret" || c.MatchBy != "email" || !c.AutoCreate ||
		!reflect.DeepEqual(c.RedirectURIs, []string{"https://a/cb"}) || !reflect.DeepEqual(c.AllowedEmailDomains, []string{"corp.com"}) {
		t.Fatalf("google conf = %+v ok=%v", c, ok)
	}
	for _, k := range []string{"github", "feishu", "wecom", "oidc", "dingtalk", "missing"} {
		if _, ok := a.ProviderConf(k); ok {
			t.Errorf("%s must be treated as not configured", k)
		}
	}
	if got := a.ProviderNames(); !reflect.DeepEqual(got, []string{"google"}) {
		t.Fatalf("ProviderNames = %v", got)
	}
}

// 默认配置：providers 为空、auto_create 默认关闭，sms 未启用。
func TestDefaultYAMLHasNoProvidersAndNoSMS(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join("..", "..", "config", "default.yaml"), filepath.Join(dir, "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Auth.ProviderNames()) != 0 || cfg.SMS.Provider != "" {
		t.Fatalf("defaults must be off: providers=%v sms=%q", cfg.Auth.ProviderNames(), cfg.SMS.Provider)
	}
	user := filepath.Join(dir, "config.yaml")
	yml := "auth:\n  providers:\n    github:\n      client_id: id\n      client_secret: ${T_GH}\n      redirect_uris: [https://a/cb]\nsms:\n  provider: webhook\n  webhook: {url: https://gw/send, secret: k}\n"
	if err := os.WriteFile(user, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("T_GH", "v")
	cfg, err = Load(filepath.Join("..", "..", "config", "default.yaml"), user)
	if err != nil {
		t.Fatal(err)
	}
	pc, ok := cfg.Auth.ProviderConf("github")
	if !ok || pc.ClientSecret != "v" || pc.AutoCreate || cfg.SMS.Webhook.URL != "https://gw/send" {
		t.Fatalf("user config not merged: %+v ok=%v sms=%+v", pc, ok, cfg.SMS)
	}
}
