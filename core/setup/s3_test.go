package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"

	"nexadmin/core/config"
)

func testRunner(t *testing.T) *Runner {
	t.Helper()
	cfg := &config.Config{UserConfigPath: filepath.Join(t.TempDir(), "config.yaml")}
	cfg.Languages = []config.Language{{Code: "zh-CN", Name: "中文"}}
	return &Runner{Cfg: cfg, Log: zap.NewNop()}
}

func baseReq() *executeReq {
	r := &executeReq{}
	r.DB = dbReq{Type: "mysql", Host: "127.0.0.1", Port: 3306, Username: "root", Database: "nexadmin"}
	r.Admin = adminReq{Username: "admin", Password: "Passw0rd!x", SiteName: "N", Language: "zh-CN", Timezone: "UTC"}
	return r
}

func TestS3OverlayWrittenWithStrictPerms(t *testing.T) {
	r := testRunner(t)
	req := baseReq()
	req.Optional = optionalReq{Storage: "s3", S3Endpoint: "https://s3.example.com", S3Bucket: "b", S3AccessKey: "AK", S3SecretKey: "SK-secret", S3Prefix: "nexadmin/", S3PathStyle: true}
	db, err := r.validate(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeConfig(r.Cfg.UserConfigPath, r.overlay(req, db)); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(r.Cfg.UserConfigPath); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("config.yaml 权限应为 0600: %v %v", st, err)
	}
	raw, _ := os.ReadFile(r.Cfg.UserConfigPath)
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	s := m["storage"].(map[string]any)
	s3 := s["s3"].(map[string]any)
	if s["driver"] != "s3" || s3["endpoint"] != "https://s3.example.com" || s3["bucket"] != "b" ||
		s3["access_key"] != "AK" || s3["secret_key"] != "SK-secret" || s3["prefix"] != "nexadmin/" || s3["path_style"] != true {
		t.Fatalf("storage 段不对（键需对齐 storage.s3.*）: %v", s)
	}
}

func TestS3NotWrittenWhenLocal(t *testing.T) {
	r := testRunner(t)
	for _, st := range []string{"", "local"} {
		req := baseReq()
		req.Optional = optionalReq{Storage: st, S3Endpoint: "https://ignored", S3Bucket: "b", S3AccessKey: "a", S3SecretKey: "s"}
		db, err := r.validate(req)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := r.overlay(req, db)["storage"]; ok {
			t.Errorf("storage=%q 不应写 storage 段", st)
		}
	}
}

func TestS3ValidationDoesNotEchoSecrets(t *testing.T) {
	r := testRunner(t)
	req := baseReq()
	req.Optional = optionalReq{Storage: "s3", S3Endpoint: "https://s3.example.com", S3Bucket: "b", S3AccessKey: "AK-visible?", S3SecretKey: ""}
	_, err := r.validate(req)
	if err == nil || strings.Contains(err.Error(), "AK-visible") || strings.Contains(err.Error(), "s3.example.com") {
		t.Fatalf("缺项应报错且不回显值: %v", err)
	}
	req.Optional = optionalReq{Storage: "gcs"}
	if _, err := r.validate(req); err == nil {
		t.Error("未知存储类型应拒绝")
	}
}

// 非交互 init --config：YAML / JSON 文件里的 optional.s3* 与向导请求体同构，且 run 的失败信息 / 日志里不出现密钥。
func TestInitFileParsesS3AndRunRedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "init.yaml")
	body := `
db: {type: mysql, host: 127.0.0.1, port: 3306, username: root, database: nexadmin}
admin: {username: admin, password: "Passw0rd!x", siteName: N, language: zh-CN, timezone: UTC}
optional: {storage: s3, s3Endpoint: "https://s3.example.com", s3Bucket: b, s3AccessKey: AK1, s3SecretKey: SK-very-secret}
`
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var req executeReq
	b, _ := os.ReadFile(f)
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(raw)
	if err := json.Unmarshal(js, &req); err != nil {
		t.Fatal(err)
	}
	if req.Optional.Storage != "s3" || req.Optional.S3SecretKey != "SK-very-secret" {
		t.Fatalf("解析不对: %+v", req.Optional)
	}

	// 让 writeConfig 之后的步骤失败（Hooks 为零值会在迁移前就出错），检查日志与步骤信息不含密钥。
	core, logs := observer.New(zap.ErrorLevel)
	r := testRunner(t)
	r.Log = zap.New(core)
	r.Cfg.UserConfigPath = filepath.Join(dir, "no-such-dir", "config.yaml") // 写配置失败，错误里带路径
	steps := r.run(t.Context(), &req)
	all, _ := json.Marshal(steps)
	for _, e := range logs.All() {
		all = append(all, []byte(e.Message)...)
		for _, fld := range e.Context {
			all = append(all, []byte(fld.String)...)
		}
	}
	for _, secret := range []string{"SK-very-secret", "AK1"} {
		if strings.Contains(string(all), secret) {
			t.Errorf("步骤结果 / 日志里不应出现 %q", secret)
		}
	}
}
