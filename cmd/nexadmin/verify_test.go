package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nexadmin/core/plugin/market"
)

func TestVerifyCmd(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cron-1.0.0.zip")
	if err := os.WriteFile(zipPath, []byte("PK\x05\x06fake-zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	sig, err := market.SignFile(zipPath, priv)
	if err != nil {
		t.Fatal(err)
	}
	sigPath := filepath.Join(dir, "cron-1.0.0.zip.sig")
	if err := os.WriteFile(sigPath, []byte(market.EncodeSignature(sig)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "market.pub")
	if err := os.WriteFile(keyPath, []byte(market.EncodePublicKey(pub)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := cmdVerify([]string{"--sig", sigPath, "--key", keyPath, zipPath}, &out); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.Contains(out.String(), "签名有效") {
		t.Errorf("output = %q", out.String())
	}

	// 篡改文件必须失败。
	if err := os.WriteFile(zipPath, []byte("PK\x05\x06fake-zip!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdVerify([]string{"--sig", sigPath, "--key", keyPath, zipPath}, &bytes.Buffer{}); err == nil {
		t.Fatal("tampered package accepted")
	}
}
