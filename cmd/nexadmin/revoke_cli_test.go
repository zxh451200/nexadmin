package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexadmin/core/plugin"
)

// sign 写入序列号 -> revoke 追加并签名清单 -> 清单与授权一起被 CheckFile 判为 revoked；
// 重复吊销被拒；换私钥不能在别人的清单上追加。
func TestRevokeCLI(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "private.pem")
	var out bytes.Buffer
	if err := run([]string{"plugin", "keygen", "--out", key}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	pubB64 := lines[len(lines)-1]
	pub, _ := base64.StdEncoding.DecodeString(pubB64)

	// 项目根：公钥写进 config/license-keys.pub，授权放 data/licenses/cron.lic，plugins/cron 是 pro 插件。
	root := filepath.Join(dir, "proj")
	for _, d := range []string{"config", "data/licenses", "plugins/cron"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config", "license-keys.pub"), []byte(pubB64+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lic := filepath.Join(root, "data", "licenses", "cron.lic")
	exp := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if err := run([]string{"plugin", "sign", "--key", key, "--plugin", "cron", "--customer", "acme", "--expires", exp, "--out", lic}, &out); err != nil {
		t.Fatal(err)
	}
	l, err := plugin.Verify([]byte(mustRead(t, lic)), "cron", time.Now(), ed25519.PublicKey(pub))
	if err != nil || !plugin.ValidSerial(l.Serial) {
		t.Fatalf("sign must write a valid serial, got %+v, %v", l, err)
	}
	if _, err := plugin.CheckFile(root, "cron", time.Now()); err != nil {
		t.Fatalf("not revoked yet: %v", err)
	}

	// 显式 --serial 合法性。
	if err := run([]string{"plugin", "sign", "--key", key, "--plugin", "cron", "--customer", "a", "--expires", exp, "--serial", "BAD"}, &out); err == nil {
		t.Fatal("invalid --serial must be rejected")
	}

	// revoke：第一次新建清单，第二次在其上追加。
	list := filepath.Join(root, "config", "license-revoked.json")
	if err := run([]string{"plugin", "revoke", "--key", key, "--serial", l.Serial, "--reason", "退款", "--plugin", "cron", "--out", list}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"plugin", "revoke", "--key", key, "--serial", "aaaa11112222", "--reason", "x", "--out", list}, &out); err != nil {
		t.Fatal(err)
	}
	rev, err := plugin.LoadRevocations(root, plugin.PublicKeys(root))
	if err != nil || rev.Seq != 2 || len(rev.Entries) != 2 {
		t.Fatalf("got %+v, %v", rev, err)
	}
	_, err = plugin.CheckFile(root, "cron", time.Now())
	var le *plugin.LicenseError
	if err == nil || !asLicErr(err, &le) || le.Status != plugin.StatusRevoked || !strings.Contains(le.Reason, "退款") {
		t.Fatalf("want revoked, got %v", err)
	}

	// 重复吊销被拒；缺参数被拒；别的私钥不能在这份清单上追加。
	if err := run([]string{"plugin", "revoke", "--key", key, "--serial", l.Serial, "--reason", "again", "--out", list}, &out); err == nil {
		t.Fatal("duplicate serial must be rejected")
	}
	if err := run([]string{"plugin", "revoke", "--key", key, "--serial", l.Serial}, &out); err == nil {
		t.Fatal("missing --reason must be rejected")
	}
	other := filepath.Join(dir, "other.pem")
	if err := run([]string{"plugin", "keygen", "--out", other}, &out); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"plugin", "revoke", "--key", other, "--serial", "cccc55556666", "--reason", "x", "--out", list}, &out); err == nil {
		t.Fatal("a different key must not append to someone else's list")
	}
}

func asLicErr(err error, target **plugin.LicenseError) bool {
	le, ok := err.(*plugin.LicenseError)
	if ok {
		*target = le
	}
	return ok
}
