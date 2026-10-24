package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexadmin/core/plugin"
)

// keygen -> sign -> Verify 全链路；keygen 不覆盖已有私钥。
func TestKeygenSignRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k", "private.pem")
	var out bytes.Buffer
	if err := run([]string{"plugin", "keygen", "--out", key}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	pub, err := base64.StdEncoding.DecodeString(lines[len(lines)-1])
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("bad public key output: %q", out.String())
	}
	if !strings.Contains(out.String(), "kid: "+plugin.KeyID(pub)) {
		t.Fatalf("keygen must print kid, got %q", out.String())
	}
	if err := run([]string{"plugin", "keygen", "--out", key}, &out); err == nil {
		t.Fatal("keygen must not overwrite an existing key")
	}
	lic := filepath.Join(dir, "cron.lic")
	exp := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	args := []string{"plugin", "sign", "--key", key, "--plugin", "cron", "--customer", "acme", "--expires", exp, "--domains", "a.com, b.com", "--out", lic}
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, lic)
	if !strings.Contains(raw, `"kid": "`+plugin.KeyID(pub)+`"`) {
		t.Fatalf("sign must write kid, got %s", raw)
	}
	l, err := plugin.Verify([]byte(raw), "cron", time.Now(), pub)
	if err != nil || l.Customer != "acme" || len(l.Domains) != 2 {
		t.Fatalf("got %+v, %v", l, err)
	}
	if err := run([]string{"plugin", "sign", "--key", key, "--plugin", "../x", "--customer", "a", "--expires", exp}, &out); err == nil {
		t.Fatal("illegal plugin id must be rejected")
	}
}

func mustRead(t *testing.T, p string) string { return read(t, p) }
