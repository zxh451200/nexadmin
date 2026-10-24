package plugin

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

// 黄金样本：testdata/golden.lic 由发行方授权后台用「测试专用」固定 seed 签出（绝不可用于真实授权），
// golden.pub 是对应公钥。发行方项目里有同一份样本，两边各读各的 testdata，靠样本保证授权文件格式一致。
func TestGoldenLicenseVerifies(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.lic")
	if err != nil {
		t.Fatal(err)
	}
	pubB64, err := os.ReadFile("testdata/golden.pub")
	if err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pubB64)))
	if err != nil || len(b) != ed25519.PublicKeySize {
		t.Fatalf("golden.pub 无效: %v", err)
	}
	pub := ed25519.PublicKey(b)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	l, err := Verify(raw, "cron", now, pub)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if l.Customer != "Golden Test Customer 测试客户" || l.ExpiresAt != "2027-01-01T00:00:00Z" || len(l.Domains) != 2 {
		t.Fatalf("license=%+v", l)
	}
	if !strings.Contains(string(raw), `"kid": "`+KeyID(pub)+`"`) {
		t.Fatal("kid 与公钥指纹不一致")
	}
	if _, err := Verify(raw, "cron", time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC), pub); err == nil {
		t.Fatal("到期后应失败")
	}
}

// 发行方后台签出的带序列号授权与吊销清单（同一把测试专用固定 seed）：框架必须能验签、解析并命中。
func TestGoldenRevocationInterop(t *testing.T) {
	pubB64, err := os.ReadFile("testdata/golden.pub")
	if err != nil {
		t.Fatal(err)
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(pubB64)))
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.PublicKey(b)
	keys := []Key{{KID: KeyID(pub), Pub: pub}}
	lic, err := os.ReadFile("testdata/golden-serial.lic")
	if err != nil {
		t.Fatal(err)
	}
	l, err := VerifyKeys(lic, "cron", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), keys)
	if err != nil || l.Serial != "golden000001" {
		t.Fatalf("license: %+v, %v", l, err)
	}
	raw, err := os.ReadFile("testdata/golden-revoked.json")
	if err != nil {
		t.Fatal(err)
	}
	rev, err := VerifyRevocations(raw, keys)
	if err != nil || rev.Seq != 3 || len(rev.Entries) != 2 {
		t.Fatalf("revocations: %+v, %v", rev, err)
	}
	if e, ok := rev.Find(l.Serial); !ok || e.Plugin != "cron" || !strings.Contains(e.Reason, "refund") {
		t.Fatalf("serial from issuer license must hit the issuer list: %+v", e)
	}
}
