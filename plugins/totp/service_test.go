package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 附录 B 的测试密钥(SHA1):base32("12345678901234567890")。
const rfcKey = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// 基本正确性:6 位数字、确定性、时变。
func TestCodeAtBasics(t *testing.T) {
	code, err := CodeAt(rfcKey, time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != Digits {
		t.Fatalf("len = %d, want %d", len(code), Digits)
	}
	again, _ := CodeAt(rfcKey, time.Unix(59, 0))
	if again != code {
		t.Fatalf("deterministic: %s vs %s", code, again)
	}
	other, _ := CodeAt(rfcKey, time.Unix(1111111109, 0))
	if other == code {
		t.Fatal("different time gave same code")
	}
}

// 验证:正码命中、±1 步长容差命中、±3 拒绝、错误位数拒绝、坏密钥拒绝。
func TestVerify(t *testing.T) {
	now := time.Unix(1111111109, 0)
	code, _ := CodeAt(rfcKey, now)
	if _, err := Verify(rfcKey, code, now); err != nil {
		t.Fatalf("exact should pass: %v", err)
	}
	prev, _ := CodeAt(rfcKey, now.Add(-StepSeconds*time.Second))
	if _, err := Verify(rfcKey, prev, now); err != nil {
		t.Fatalf("tolerance should pass: %v", err)
	}
	far, _ := CodeAt(rfcKey, now.Add(-3*StepSeconds*time.Second))
	if _, err := Verify(rfcKey, far, now); err == nil {
		t.Fatal("far code should fail")
	}
	if _, err := Verify(rfcKey, "123", now); err == nil {
		t.Fatal("short code should fail")
	}
	if _, err := Verify("bad$$secret", code, now); err == nil {
		t.Fatal("bad secret should fail")
	}
}

// otpauth URI 格式。
func TestOTPAuthURI(t *testing.T) {
	uri := OTPAuthURI("NexAdmin", "admin@example.com", "ABCDEF")
	for _, want := range []string{"otpauth://totp/", "NexAdmin", "admin@example.com", "secret=ABCDEF", "digits=6", "period=30"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("uri missing %q: %s", want, uri)
		}
	}
}
