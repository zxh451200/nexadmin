package config

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"nexadmin/core/response"
	"nexadmin/core/secret"
)

func useTestKey(t *testing.T) {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c, err := secret.New(k)
	if err != nil {
		t.Fatal(err)
	}
	secret.SetDefault(c)
	t.Cleanup(func() { secret.SetDefault(nil) })
}

func sample() map[string]any {
	return map[string]any{
		"enabled": true, "apiKey": "sk-live-123", "model": "qwen",
		"providers": map[string]any{"oidc": map[string]any{"issuer": "https://i", "clientSecret": "oidc-secret"}},
		"password":  "", "secretKey": "s3-secret",
	}
}

func TestEncryptSecretsOnlyTouchesSecretFields(t *testing.T) {
	useTestKey(t)
	out, err := encryptSecrets(sample())
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	raw, _ := json.Marshal(m)
	for _, plain := range []string{"sk-live-123", "oidc-secret", "s3-secret"} {
		if strings.Contains(string(raw), plain) {
			t.Errorf("plaintext %q leaked into stored JSON: %s", plain, raw)
		}
	}
	if !secret.IsEncrypted(m["apiKey"].(string)) || !secret.IsEncrypted(m["secretKey"].(string)) {
		t.Fatal("apiKey/secretKey must be encrypted")
	}
	oidc := m["providers"].(map[string]any)["oidc"].(map[string]any)
	if !secret.IsEncrypted(oidc["clientSecret"].(string)) || oidc["issuer"] != "https://i" {
		t.Fatalf("nested handling wrong: %v", oidc)
	}
	if m["password"] != "" || m["model"] != "qwen" || m["enabled"] != true {
		t.Fatalf("empty secrets and non-secret fields must be untouched: %v", m)
	}
	if sample()["apiKey"] != "sk-live-123" {
		t.Fatal("input must not be mutated")
	}
}

func TestStoredCipherDecryptsBackAndMasksOnOutput(t *testing.T) {
	useTestKey(t)
	enc, _ := encryptSecrets(sample())
	raw, _ := json.Marshal(enc)
	var saved map[string]any
	_ = json.Unmarshal(raw, &saved)
	new(Service).decryptSecrets("ai", saved)
	if saved["apiKey"] != "sk-live-123" || saved["providers"].(map[string]any)["oidc"].(map[string]any)["clientSecret"] != "oidc-secret" {
		t.Fatalf("round trip failed: %v", saved)
	}
	masked, _ := json.Marshal(maskSecrets(saved))
	for _, plain := range []string{"sk-live-123", "oidc-secret", "s3-secret", "enc:v1:"} {
		if strings.Contains(string(masked), plain) {
			t.Errorf("API output leaks %q: %s", plain, masked)
		}
	}
	if !strings.Contains(string(masked), secretMask) {
		t.Fatal("secrets must be masked")
	}
}

func TestLegacyPlaintextReadAndEncryptedOnNextSave(t *testing.T) {
	useTestKey(t)
	legacy := map[string]any{"apiKey": "old-plain-key", "model": "m"}
	new(Service).decryptSecrets("ai", legacy)
	if legacy["apiKey"] != "old-plain-key" {
		t.Fatalf("plaintext must be read as-is, got %v", legacy["apiKey"])
	}
	// 前端回传占位符 -> keepSecrets 保留旧明文 -> 保存时加密
	next := keepSecrets(map[string]any{"apiKey": secretMask, "model": "m2"}, legacy).(map[string]any)
	stored, err := encryptSecrets(merge(legacy, next))
	if err != nil {
		t.Fatal(err)
	}
	got := stored.(map[string]any)["apiKey"].(string)
	if !secret.IsEncrypted(got) {
		t.Fatalf("legacy secret must be encrypted after save, got %q", got)
	}
	if plain, err := secret.Decrypt(got); err != nil || plain != "old-plain-key" {
		t.Fatalf("decrypt after save: %q %v", plain, err)
	}
}

func TestTamperedOrForeignCipherBecomesUnset(t *testing.T) {
	useTestKey(t)
	enc, _ := secret.Encrypt("v")
	for name, bad := range map[string]string{"tampered": enc[:len(enc)-2] + "AA", "foreign": "enc:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		m := map[string]any{"apiKey": bad}
		new(Service).decryptSecrets("ai", m)
		if m["apiKey"] != "" {
			t.Errorf("%s: want empty, got %v", name, m["apiKey"])
		}
	}
}

func TestEncryptFailsClosedWithoutKey(t *testing.T) {
	secret.SetDefault(nil)
	if _, err := encryptSecrets(sample()); err == nil {
		t.Fatal("must refuse to store secrets in plaintext when no key is available")
	}
}

// 文件存储以 config/config.yaml 为准：设置页保存会被明确拒绝（不触库），而不是“保存成功却不生效”。
func TestSaveStorageSectionRejected(t *testing.T) {
	s := &Service{} // 拒绝发生在任何数据库访问之前
	_, err := s.SaveSection(context.Background(), "storage", map[string]any{"type": "s3", "secretKey": "x"})
	var re *response.Error
	if !errors.As(err, &re) || re.Status != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
	if strings.Contains(err.Error(), "secretKey") || strings.Contains(re.Error(), "\"x\"") {
		t.Errorf("错误信息不应回显提交的值: %v", err)
	}
}
