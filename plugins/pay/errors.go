package pay

import (
	"errors"
	"regexp"

	"nexadmin/core/response"
)

// 错误 key 即契约的 PAY_* 错误码（02-openapi-pay-v1.yaml ApiError.errorCode），
// 文案单住 core/i18n/locales；HTTP 状态码沿用框架 response 口径。
const (
	errBizRequired            = "pay.PAY_INVALID_AMOUNT"
	errInvalidAmount          = "pay.PAY_INVALID_AMOUNT"
	errCurrencyUnsupported    = "pay.PAY_CURRENCY_UNSUPPORTED"
	errFxRateMissing          = "pay.PAY_FX_RATE_MISSING"
	errStateConflict          = "pay.PAY_STATE_CONFLICT"
	errExpired                = "pay.PAY_EXPIRED"
	errRefundExceeds          = "pay.PAY_REFUND_EXCEEDS"
	errSignInvalid            = "pay.PAY_SIGN_INVALID"
	errChannelUnavailable     = "pay.PAY_CHANNEL_UNAVAILABLE"
	errChannelError           = "pay.PAY_CHANNEL_ERROR"
	errNotFound               = "pay.PAY_NOT_FOUND"
	errTenantMismatch         = "pay.PAY_TENANT_MISMATCH"
	errIdempotencyConflict    = "pay.PAY_IDEMPOTENCY_CONFLICT"
	errInternal               = "pay.PAY_INTERNAL"
	errNotRefundable          = errStateConflict
	errNotClosable            = errStateConflict
	errRefundRejected         = errChannelError
	errNoChannel              = errChannelUnavailable
	errChannelNotFound        = errNotFound
	errChannelInvalid         = "pay.PAY_INVALID_AMOUNT"
	errChannelUnknownProvider = "pay.PAY_CHANNEL_UNAVAILABLE"
	errChannelCodeTaken       = errStateConflict
	errTenantRequired         = errTenantMismatch
)

// 回调审计 result 取值（契约 Callback.result）。
const (
	ResultAccepted     = "accepted"
	ResultDuplicate    = "duplicate"
	ResultSignInvalid  = "sign_invalid"
	ResultOrderMissing = "order_not_found"
)

// 抽佣规则状态。
const (
	RuleActive   = "active"
	RuleDisabled = "disabled"
)

var errMockPending = errors.New("mock: pending")

// channelCodeRe 渠道实例编码：小写字母/数字/连字符，2-32 位（公开回调路径段，从紧）。
var channelCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

// channelNotifyURL 是渠道异步回调地址的站点内路径（Provider 可拼上站点域名）。
func channelNotifyURL(channelCode string) string {
	return "/api/v1/plugins/pay/notify/" + channelCode
}

// idemConflict 是请求级幂等冲突（同键异体）。
func idemConflict() *response.Error { return response.New(422, errIdempotencyConflict) }
