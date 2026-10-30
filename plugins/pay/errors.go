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
//
// 命名规范（新增取值前先对照，别另起一套）：
//  1. 小写 ASCII + 下划线 snake_case，只用字母/数字/`_`；落库列 pay_callback.result 是 size:32，取值别超过 32 字符。
//  2. 语义 = 对这条回调的处置结论（我们做了什么），不是 HTTP 状态码，也不写渠道名：
//     accepted 入账 / duplicate 已处理过 / sign_invalid 验签失败 / order_not_found 找不到资金单。
//  3. 一个取值一个常量，常量名 `Result<语义>`，按语义分组排序；禁止在业务代码里写字面量。
//  4. 需要带状态后缀时用固定前缀拼：`ignored_state_<order.status>`（如 ignored_state_paid），
//     前缀本身也要提成常量，别散落在两个文件里。
//  5. 与错误码分开：result 是小写 snake_case 的“结论”；错误码是 `PAY_*` 大写常量（见上方 error key），
//     同一件事两边可以各有一个名字，但不能混用大小写风格。
const (
	ResultAccepted       = "accepted"
	ResultDuplicate      = "duplicate"
	ResultSignInvalid    = "sign_invalid"
	ResultOrderMissing   = "order_not_found"
	ResultAmountMismatch = "amount_mismatch"
	// ResultIgnoredStatePrefix 后面拼资金单状态，例如 ignored_state_paid。
	ResultIgnoredStatePrefix = "ignored_state_"
)

// 抽佣规则状态。
const (
	RuleActive   = "active"
	RuleDisabled = "disabled"
)

var errMockPending = errors.New("mock: pending")

// errCallbackAmount 是「回调金额与资金单不一致」的哨兵：调用方用 errors.Is 区分，
// 好把回调审计结果记成 ResultAmountMismatch，而不是和其它失败混在一起。
var errCallbackAmount = errors.New("pay: callback amount mismatch")

// channelCodeRe 渠道实例编码：小写字母/数字/连字符，2-32 位（公开回调路径段，从紧）。
var channelCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,31}$`)

// channelNotifyURL 是渠道异步回调地址的站点内路径（Provider 可拼上站点域名）。
func channelNotifyURL(channelCode string) string {
	return "/api/v1/plugins/pay/notify/" + channelCode
}

// idemConflict 是请求级幂等冲突（同键异体）。
func idemConflict() *response.Error { return response.New(422, errIdempotencyConflict) }

// callbackAmountMismatch 是渠道回调金额与资金单不一致：拒绝入账，留给人工核对，不重试。
// 复用 PAY_INVALID_AMOUNT 的文案，避免为此新增一个契约外的错误码；
// 但必须把 errCallbackAmount 包进去，HandleCallback 靠 errors.Is 才能把审计结果记成
// ResultAmountMismatch，否则会被当成 duplicate。
func callbackAmountMismatch() *response.Error {
	return response.New(400, errInvalidAmount).Wrap(errCallbackAmount)
}
