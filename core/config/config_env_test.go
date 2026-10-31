package config

// NEXADMIN_* 环境变量覆盖矩阵：viper AutomaticEnv + Unmarshal 的组合是已知坑区，
// 每个主要配置段至少一条“env 覆盖生效”用例 + 一条“错误键名不生效”用例（回归防线）。

import (
	"os"
	"testing"
	"time"
)

// setEnv 批量设置环境变量并返回清理函数。
func setEnv(t *testing.T, kv map[string]string) func() {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
	return func() {
		for k := range kv {
			_ = os.Unsetenv(k)
		}
	}
}

// writeConfigFiles 在临时目录写出 default + user 两份配置，返回两个路径。
func writeConfigFiles(t *testing.T, defaultYAML, userYAML string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	def := dir + "/default.yaml"
	usr := dir + "/config.yaml"
	if err := os.WriteFile(def, []byte(defaultYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(usr, []byte(userYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return def, usr
}

// TestEnvOverrideMatrix 核对主要配置段的 NEXADMIN_* 覆盖都真实生效。
// 这是对 viper AutomaticEnv + Unmarshal 行为的回归防线：任何一侧退化都会在这里红。
func TestEnvOverrideMatrix(t *testing.T) {
	const defYAML = `
app:
  env: dev
  timezone: UTC
server:
  port: 8080
  trusted_proxies: []
database:
  type: mysql
  host: 127.0.0.1
  port: 3306
  node_id: 0
cache:
  driver: memory
  addr:
auth:
  access_ttl: 30m
  refresh_ttl: 168h
backup:
  keep_days: 30
  keep_count: 10
region:
  source_url: https://example.com/from-file.json
  source_license: MIT
languages:
  - code: zh-CN
    name: 简体中文
    dir: ltr
    default: true
`
	const usrYAML = `
database:
  password: from-file
`
	cleanup := setEnv(t, map[string]string{
		"NEXADMIN_APP_ENV":                "production",
		"NEXADMIN_APP_TIMEZONE":           "Asia/Shanghai",
		"NEXADMIN_SERVER_PORT":            "18089",
		"NEXADMIN_SERVER_TRUSTED_PROXIES": "127.0.0.1,10.0.0.0/8",
		"NEXADMIN_DATABASE_TYPE":          "postgres",
		"NEXADMIN_DATABASE_PASSWORD":      "from-env",
		"NEXADMIN_DATABASE_NODE_ID":       "7",
		"NEXADMIN_CACHE_DRIVER":           "redis",
		"NEXADMIN_CACHE_ADDR":             "valkey:6379",
		"NEXADMIN_AUTH_ACCESS_TTL":        "45m",
		"NEXADMIN_AUTH_REFRESH_TTL":       "240h",
		"NEXADMIN_BACKUP_KEEP_DAYS":       "7",
		"NEXADMIN_BACKUP_KEEP_COUNT":      "3",
		"NEXADMIN_REGION_SOURCE_URL":      "https://example.com/from-env.json",
	})
	defer cleanup()

	defPath, usrPath := writeConfigFiles(t, defYAML, usrYAML)
	cfg, err := Load(defPath, usrPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.App.Env != "production" {
		t.Errorf("app.env env override: got %q", cfg.App.Env)
	}
	if cfg.App.Timezone != "Asia/Shanghai" {
		t.Errorf("app.timezone env override: got %q", cfg.App.Timezone)
	}
	if cfg.Server.Port != 18089 {
		t.Errorf("server.port env override: got %d", cfg.Server.Port)
	}
	if len(cfg.Server.TrustedProxies) != 2 {
		t.Errorf("server.trusted_proxies env list override: got %v", cfg.Server.TrustedProxies)
	}
	if cfg.Database.Type != "postgres" {
		t.Errorf("database.type env override: got %q", cfg.Database.Type)
	}
	if cfg.Database.Password != "from-env" {
		t.Errorf("database.password env override: got %q", cfg.Database.Password)
	}
	if cfg.Database.NodeID != 7 {
		t.Errorf("database.node_id env override: got %d", cfg.Database.NodeID)
	}
	if cfg.Cache.Driver != "redis" {
		t.Errorf("cache.driver env override: got %q", cfg.Cache.Driver)
	}
	if cfg.Auth.AccessTTL != 45*time.Minute {
		t.Errorf("auth.access_ttl env override: got %v", cfg.Auth.AccessTTL)
	}
	if cfg.Auth.RefreshTTL != 240*time.Hour {
		t.Errorf("auth.refresh_ttl env override: got %v", cfg.Auth.RefreshTTL)
	}
	if cfg.Backup.KeepDays != 7 || cfg.Backup.KeepCount != 3 {
		t.Errorf("backup.* env override: got %d/%d", cfg.Backup.KeepDays, cfg.Backup.KeepCount)
	}
	if cfg.Region.SourceURL != "https://example.com/from-env.json" {
		t.Errorf("region.source_url env override: got %q", cfg.Region.SourceURL)
	}
	if cfg.Region.SourceLicense != "MIT" {
		t.Errorf("region.source_license 应从文件读到 MIT，得到 %q", cfg.Region.SourceLicense)
	}
}

// TestEnvWrongKeyNameDoesNotApply 键名写错（段名或分隔符不对）时不得悄悄生效，
// 也不得把配置弄坏——写错的变量应被忽略，回落到文件值。
func TestEnvWrongKeyNameDoesNotApply(t *testing.T) {
	const defYAML = `
app:
  env: dev
server:
  port: 8080
database:
  type: mysql
  password: from-file
cache:
  driver: memory
auth:
  access_ttl: 30m
  refresh_ttl: 168h
languages:
  - code: zh-CN
    name: 简体中文
    dir: ltr
    default: true
`
	cleanup := setEnv(t, map[string]string{
		"NEXADMIN_APP_PORT":       "9999",  // 段名写错（server 才对）
		"NEXADMIN_SERVER_PORT_XX": "9999",  // 键名多了一级
		"NEXADMIN_DB_PASSWORD":    "wrong", // 段名缩写
	})
	defer cleanup()

	defPath, usrPath := writeConfigFiles(t, defYAML, "")
	cfg, err := Load(defPath, usrPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("wrong-key env must not apply, server.port = %d", cfg.Server.Port)
	}
	if cfg.Database.Password != "from-file" {
		t.Errorf("wrong-key env must not apply, database.password = %q", cfg.Database.Password)
	}
}

// TestUserFileOverridesDefault 用户文件覆盖框架默认值（合并语义的基线用例）。
func TestUserFileOverridesDefault(t *testing.T) {
	const defYAML = `
app:
  env: dev
server:
  port: 8080
database:
  type: mysql
  host: 127.0.0.1
  name: nexadmin
cache:
  driver: memory
auth:
  access_ttl: 30m
  refresh_ttl: 168h
languages:
  - code: zh-CN
    name: 简体中文
    dir: ltr
    default: true
`
	const usrYAML = `
server:
  port: 18089
`
	cleanup := setEnv(t, map[string]string{})
	defer cleanup()

	defPath, usrPath := writeConfigFiles(t, defYAML, usrYAML)
	cfg, err := Load(defPath, usrPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 18089 {
		t.Errorf("user file override: port = %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "127.0.0.1" || cfg.Database.Name != "nexadmin" {
		t.Errorf("user file merge must keep defaults: %+v", cfg.Database)
	}
}
