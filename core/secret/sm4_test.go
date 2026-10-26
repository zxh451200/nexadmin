package secret

import (
	"crypto/rand"
	"strings"
	"testing"
)

func newSM4Cipher(t *testing.T) *Cipher {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c, err := NewWithAlgorithm(k, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// SM4 模式：往返 + 密文带 v2 前缀。
func TestSM4RoundTrip(t *testing.T) {
	c := newSM4Cipher(t)
	for _, plain := range []string{"password123", "中文口令 with spaces", strings.Repeat("y", 5000)} {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(enc, PrefixSM4) {
			t.Fatalf("ciphertext %q should have v2 prefix", enc[:12])
		}
		got, err := c.Decrypt(enc)
		if err != nil || got != plain {
			t.Fatalf("roundtrip failed: %v", err)
		}
	}
}

// SM4 模式下旧 v1（AES）密文仍可解：算法切换不破坏存量数据。
func TestSM4DecryptsV1Ciphertext(t *testing.T) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	aesC, err := New(k)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := aesC.Encrypt("legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	sm4C, err := NewWithAlgorithm(k, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sm4C.Decrypt(enc)
	if err != nil || got != "legacy-value" {
		t.Fatalf("v1 ciphertext should decrypt in sm4 mode: %v", err)
	}
}

// 反向：AES 模式的 Cipher 拒绝 v2 密文（未启用 SM4 一律 ErrDecrypt）。
func TestAESModeRejectsV2(t *testing.T) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	sm4C, err := NewWithAlgorithm(k, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := sm4C.Encrypt("sm4-value")
	if err != nil {
		t.Fatal(err)
	}
	aesC, err := New(k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aesC.Decrypt(enc); err != ErrDecrypt {
		t.Fatalf("want ErrDecrypt, got %v", err)
	}
}

// 篡改检测：v2 密文任何字节被改都解不开。
func TestSM4TamperDetected(t *testing.T) {
	c := newSM4Cipher(t)
	enc, err := c.Encrypt("tamper-me")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(strings.TrimPrefix(enc, PrefixSM4))
	body[len(body)-1] ^= 0x01
	if _, err := c.Decrypt(PrefixSM4 + string(body)); err != ErrDecrypt {
		t.Fatalf("want ErrDecrypt, got %v", err)
	}
}

// 同一主密钥的 SM4 子密钥确定（换进程可解），不同主密钥互不相通。
func TestSM4KeyDerivationStable(t *testing.T) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c1, err := NewWithAlgorithm(k, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := NewWithAlgorithm(k, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c1.Encrypt("stable")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c2.Decrypt(enc); err != nil || got != "stable" {
		t.Fatalf("same master should decrypt: %v", err)
	}

	k2 := make([]byte, 32)
	if _, err := rand.Read(k2); err != nil {
		t.Fatal(err)
	}
	c3, err := NewWithAlgorithm(k2, AlgoSM4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c3.Decrypt(enc); err != ErrDecrypt {
		t.Fatalf("different master must fail, got %v", err)
	}
}

// 非法算法名拒绝；空算法回落 AES；IsEncrypted 认两种前缀。
func TestAlgorithmValidation(t *testing.T) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithAlgorithm(k, "des-cbc"); err == nil {
		t.Fatal("unknown algorithm accepted")
	}
	c, err := NewWithAlgorithm(k, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.algo != AlgoAES {
		t.Fatalf("empty algo should default to aes, got %q", c.algo)
	}
	if !IsEncrypted(Prefix+"x") || !IsEncrypted(PrefixSM4+"x") || IsEncrypted("plain") {
		t.Fatal("IsEncrypted prefix check broken")
	}
}
