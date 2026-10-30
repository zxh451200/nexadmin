package pay

import (
	"crypto/sha256"
	"encoding/hex"
)

// refundIdempotencyKey 生成渠道侧退款幂等号（支付宝 out_request_no / 微信 out_refund_no）。
//
// 同一支付单 + 同一业务单据反复退款重试必须得到同一个键，否则渠道会当成两笔退款；
// 渠道对该字段有长度上限（≤64），所以取哈希的前 32 位而不是拼明文。
func refundIdempotencyKey(paymentNo, bizType, bizNo string) string {
	sum := sha256.Sum256([]byte(paymentNo + "\x1f" + bizType + "\x1f" + bizNo))
	return "rf" + hex.EncodeToString(sum[:16])
}
