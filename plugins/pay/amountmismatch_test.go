package pay

import (
	"errors"
	"testing"

	"nexadmin/core/response"
)

// 回调金额不一致必须是可识别的哨兵：HandleCallback 用 errors.Is 决定审计结果，
// 包一层 response.Error 后若丢掉 Unwrap，就会被误记成 duplicate。
func TestCallbackAmountMismatchIsRecognizable(t *testing.T) {
	err := callbackAmountMismatch()
	if !errors.Is(err, errCallbackAmount) {
		t.Fatal("errors.Is(err, errCallbackAmount) 必须为 true，否则审计结果会退化成 duplicate")
	}
	var re *response.Error
	if !errors.As(err, &re) || re.Status != 400 {
		t.Fatalf("仍应是带 HTTP 语义的 response.Error(400)，实际 %#v", re)
	}
	if ResultAmountMismatch != "amount_mismatch" {
		t.Fatalf("审计结果取值被改动: %q", ResultAmountMismatch)
	}
}
