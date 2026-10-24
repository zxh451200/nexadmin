package secret

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newCipher(t *testing.T) *Cipher {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c, err := New(k)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newCipher(t)
	for _, plain := range []string{"sk-abc123", "中文密钥 with spaces", strings.Repeat("x", 10000), "enc:v1:looks-like-cipher"} {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(enc, Prefix) || strings.Contains(enc, plain) && len(plain) > 3 {
			t.Fatalf("bad ciphertext %q", enc)
		}
		got, err := c.Decrypt(enc)
		if err != nil || got != plain {
			t.Fatalf("round trip: got %q err %v want %q", got, err, plain)
		}
	}
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("nonce must be random: identical ciphertexts")
	}
}

func TestTamperRejected(t *testing.T) {
	c := newCipher(t)
	enc, _ := c.Encrypt("top-secret")
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(enc, Prefix))
	for _, i := range []int{0, len(raw) / 2, len(raw) - 1} { // nonce、密文、tag 各翻一位
		bad := bytes.Clone(raw)
		bad[i] ^= 1
		if _, err := c.Decrypt(Prefix + base64.RawURLEncoding.EncodeToString(bad)); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("flip byte %d: want ErrDecrypt, got %v", i, err)
		}
	}
	for _, bad := range []string{Prefix, Prefix + "!!!", Prefix + base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if _, err := c.Decrypt(bad); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("%q: want ErrDecrypt, got %v", bad, err)
		}
	}
}

func TestWrongKeyFails(t *testing.T) {
	enc, _ := newCipher(t).Encrypt("top-secret")
	if _, err := newCipher(t).Decrypt(enc); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("want ErrDecrypt with wrong key, got %v", err)
	}
}

func TestPlaintextCompat(t *testing.T) {
	c := newCipher(t)
	if got, err := c.Decrypt("legacy-plain"); err != nil || got != "legacy-plain" {
		t.Fatalf("plaintext must pass through: %q %v", got, err)
	}
	if got, err := c.Decrypt(""); err != nil || got != "" {
		t.Fatalf("empty: %q %v", got, err)
	}
}

func TestDefaultNotInitialized(t *testing.T) {
	SetDefault(nil)
	if _, err := Encrypt("x"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("want ErrNotInitialized, got %v", err)
	}
	if got, err := Decrypt("plain"); err != nil || got != "plain" {
		t.Fatal("plaintext decrypt must not need a key")
	}
	if _, err := Decrypt(Prefix + "AAAA"); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("want ErrNotInitialized, got %v", err)
	}
}

func TestKeyFileGeneratedWith0600AndReused(t *testing.T) {
	t.Setenv(EnvKey, "")
	dir := filepath.Join(t.TempDir(), "data")
	k1, err := LoadKey(dir)
	if err != nil || len(k1) != 32 {
		t.Fatalf("generate: %v len=%d", err, len(k1))
	}
	st, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v, want 0600", st.Mode().Perm())
	}
	k2, err := LoadKey(dir)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("second load must reuse key: %v", err)
	}
}

func TestEnvKeyOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKey, "")
	fileKey, _ := LoadKey(dir)
	envKey := bytes.Repeat([]byte{7}, 32)
	for _, v := range []string{hex.EncodeToString(envKey), base64.StdEncoding.EncodeToString(envKey)} {
		t.Setenv(EnvKey, v)
		got, err := LoadKey(dir)
		if err != nil || !bytes.Equal(got, envKey) || bytes.Equal(got, fileKey) {
			t.Fatalf("env %q not honoured: %v", v, err)
		}
	}
	t.Setenv(EnvKey, "too-short")
	if _, err := LoadKey(dir); err == nil {
		t.Fatal("invalid env key must be rejected")
	}
}
