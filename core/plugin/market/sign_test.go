package market

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestSignVerifyFile(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pkg.zip")
	if err := os.WriteFile(path, []byte("PK\x05\x06fake-zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	sig, err := SignFile(path, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, sig, pub); err != nil {
		t.Fatalf("verify: %v", err)
	}

	// 篡改一个字节必须失败。
	if err := os.WriteFile(path, []byte("PK\x05\x06fake-zip!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, sig, pub); err == nil {
		t.Fatal("tampered file verified")
	}

	// 用另一把公钥也必须失败。
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := os.WriteFile(path, []byte("PK\x05\x06fake-zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFile(path, sig, pub2); err == nil {
		t.Fatal("wrong key verified")
	}

	// base64 编解码往返。
	pub2b, err := DecodePublicKey(EncodePublicKey(pub))
	if err != nil || string(pub2b) != string(pub) {
		t.Fatalf("public key roundtrip: %v", err)
	}
	sig2, err := DecodeSignature(EncodeSignature(sig))
	if err != nil || string(sig2) != string(sig) {
		t.Fatalf("signature roundtrip: %v", err)
	}
}

func TestLoadSigningKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.pem")
	raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSigningKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(priv) {
		t.Fatal("loaded key differs")
	}
	if _, err := LoadSigningKey(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("missing key accepted")
	}
}
