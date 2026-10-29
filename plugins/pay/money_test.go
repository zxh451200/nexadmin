package pay

import (
	"testing"
	"time"
)

// half-even（银行家舍入）：恰为半时舍入到偶数，其余四舍五入。契约 §金额与舍入。
func TestMulBpHalfEven(t *testing.T) {
	cases := []struct {
		amount, bp, want int64
	}{
		{1050, 500, 52},   // 52.5 → 52（偶）
		{1150, 500, 58},   // 57.5 → 58（偶）
		{100, 1250, 12},   // 12.5 → 12
		{100, 3750, 38},   // 37.5 → 38
		{100, 1249, 12},   // 12.49 → 12
		{100, 1251, 13},   // 12.51 → 13
		{1, 1, 0},         // 0.0001 → 0
		{199, 10000, 199}, // 100% 原样
		{-1050, 500, -52}, // 负数对称：-52.5 → -52（偶）
		{-1150, 500, -58},
	}
	for _, c := range cases {
		if got := MulBp(c.amount, c.bp); got != c.want {
			t.Fatalf("MulBp(%d, %d) = %d, want %d", c.amount, c.bp, got, c.want)
		}
	}
	// 大数不溢出：4e15 × 9999 ≈ 4e19 已超 int64，big.Int 路径必须正确。
	got := MulBp(4_000_000_000_000_000, 9999)
	if want := int64(3_999_600_000_000_000); got != want {
		t.Fatalf("MulBp(big) = %d, want %d", got, want)
	}
}

func TestFxConvert(t *testing.T) {
	cases := []struct {
		amount, micro, want int64
	}{
		{10000, 7250000, 72500}, // ×7.25
		{1, 1000000, 1},         // ×1.000000 原样
		{1, 500000, 0},          // 0.5 → 0（偶）
		{3, 500000, 2},          // 1.5 → 2（偶）
		{100, 3333333, 333},     // ×3.333333
	}
	for _, c := range cases {
		if got := FxConvert(c.amount, c.micro); got != c.want {
			t.Fatalf("FxConvert(%d, %d) = %d, want %d", c.amount, c.micro, got, c.want)
		}
	}
}

func TestFeeOf(t *testing.T) {
	if got := feeOf(10000, nil); got != 0 {
		t.Fatalf("no rule should be zero fee, got %d", got)
	}
	rule := &CommissionRule{RateBp: 60, FixedMinor: 100} // 0.6% + 1 分
	if got := feeOf(10000, rule); got != 160 {
		t.Fatalf("feeOf = %d, want 160", got)
	}
	// 退款回手续费与退款额同口径，且不超过已收手续费。
	if got := feeRefundOf(5000, rule, 160); got != 130 {
		t.Fatalf("feeRefundOf = %d, want 130", got)
	}
	if got := feeRefundOf(10000, rule, 50); got != 50 {
		t.Fatalf("feeRefundOf cap = %d, want 50", got)
	}
}

func TestNormalizeFx(t *testing.T) {
	// 同币种缺省 1.000000。
	fx, orig, cur, err := normalizeFx(0, 0, "", "CNY")
	if err != nil || fx != FxUnity || orig != 0 || cur != "" {
		t.Fatalf("same-currency default: fx=%d orig=%d cur=%q err=%v", fx, orig, cur, err)
	}
	// 外币必须带汇率快照。
	if _, _, _, err := normalizeFx(0, 100, "USD", "CNY"); err == nil {
		t.Fatal("missing fx for foreign currency should fail")
	}
	// 外币带汇率：原样保留。
	fx, orig, cur, err = normalizeFx(7250000, 100, "USD", "CNY")
	if err != nil || fx != 7250000 || orig != 100 || cur != "USD" {
		t.Fatalf("foreign currency: fx=%d orig=%d cur=%q err=%v", fx, orig, cur, err)
	}
}

func TestSettleableAndFreezable(t *testing.T) {
	for _, st := range []string{StatusSucceeded, StatusRefunding, StatusPartiallyRefunded} {
		if !Settleable(st) {
			t.Fatalf("Settleable(%q) = false", st)
		}
	}
	for _, st := range []string{StatusPending, StatusFailed, StatusClosed, StatusRefunded} {
		if Settleable(st) {
			t.Fatalf("Settleable(%q) = true", st)
		}
	}
	for _, st := range []string{AuthAuthorized, AuthPartialCaptured} {
		if !Freezable(st) {
			t.Fatalf("Freezable(%q) = false", st)
		}
	}
	for _, st := range []string{AuthCaptured, AuthVoided, AuthExpired} {
		if Freezable(st) {
			t.Fatalf("Freezable(%q) = true", st)
		}
	}
}

func TestOutboxBackoff(t *testing.T) {
	if got := outboxBackoff(1); got != 2*time.Second {
		t.Fatalf("backoff(1) = %v", got)
	}
	if got := outboxBackoff(20); got != 10*time.Minute {
		t.Fatalf("backoff(20) = %v, want cap 10m", got)
	}
}
