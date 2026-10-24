package config

import (
	"encoding/json"
	"strings"
	"testing"

	"nexadmin/core/secret"
)

func notifySample() map[string]any {
	return map[string]any{
		"mail":        map[string]any{"host": "smtp.example.org", "port": float64(465), "security": "ssl", "username": "u", "password": "smtp-pw", "fromName": "Nex", "fromAddr": "noreply@example.org"},
		"robots":      []any{map[string]any{"name": "ops", "type": "dingtalk", "url": "https://oapi.dingtalk.com/robot/send?access_token=t", "secretKey": "SEC-ops", "markdown": false}},
		"accountMail": false,
	}
}

func TestNotifySecretsMaskedEncryptedAndKept(t *testing.T) {
	useTestKey(t)
	m, err := normalizeNotify(notifySample())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := encryptSecrets(m)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(enc)
	for _, plain := range []string{"smtp-pw", "SEC-ops"} {
		if strings.Contains(string(raw), plain) {
			t.Fatalf("%q stored in plaintext: %s", plain, raw)
		}
	}
	masked := maskSecrets(m).(map[string]any)
	if masked["mail"].(map[string]any)["password"] != secretMask || masked["robots"].([]any)[0].(map[string]any)["secretKey"] != secretMask {
		t.Fatalf("secrets must be masked: %v", masked)
	}
	// 前端把打码值原样传回，且机器人顺序被调换 / 新增：按 name 找回旧密钥。
	body := map[string]any{"robots": []any{
		map[string]any{"name": "new", "type": "wecom", "url": "https://qyapi.weixin.qq.com/x", "secretKey": secretMask},
		map[string]any{"name": "ops", "type": "dingtalk", "url": "https://oapi.dingtalk.com/robot/send?access_token=t", "secretKey": secretMask},
	}}
	kept := keepSecrets(body, m).(map[string]any)["robots"].([]any)
	if kept[1].(map[string]any)["secretKey"] != "SEC-ops" || kept[0].(map[string]any)["secretKey"] != "" {
		t.Fatalf("keepSecrets by name failed: %v", kept)
	}
	// 解密回环
	s := &Service{}
	dec := map[string]any{}
	_ = json.Unmarshal(raw, &dec)
	s.decryptSecrets("notify", dec)
	if dec["robots"].([]any)[0].(map[string]any)["secretKey"] != "SEC-ops" || dec["mail"].(map[string]any)["password"] != "smtp-pw" {
		t.Fatalf("decrypt round trip failed: %v", dec)
	}
	_ = secret.Default()
}

func TestNormalizeNotifyRejectsBadRobots(t *testing.T) {
	cases := map[string]string{
		"http":     "http://oapi.dingtalk.com/robot/send",
		"loopback": "https://127.0.0.1/x",
		"internal": "https://10.0.0.5/hook",
		"meta":     "https://169.254.169.254/latest",
	}
	for name, u := range cases {
		m := notifySample()
		m["robots"] = []any{map[string]any{"name": "r", "type": "dingtalk", "url": u}}
		if _, err := normalizeNotify(m); err == nil {
			t.Errorf("%s: %s should be rejected", name, u)
		}
	}
	m := notifySample()
	r := m["robots"].([]any)[0]
	m["robots"] = []any{r, r}
	if _, err := normalizeNotify(m); err == nil {
		t.Error("duplicate names must be rejected")
	}
	m = notifySample()
	m["mail"].(map[string]any)["fromAddr"] = "bad\r\nBcc: x@y"
	if _, err := normalizeNotify(m); err == nil {
		t.Error("CRLF in from address must be rejected")
	}
	m = notifySample()
	m["robots"].([]any)[0].(map[string]any)["extra"] = "dropped"
	out, err := normalizeNotify(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["robots"].([]any)[0].(map[string]any)["extra"]; ok {
		t.Error("unknown robot fields must be dropped")
	}
}
