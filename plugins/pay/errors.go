package pay

import (
	"errors"
	"regexp"

	"nexadmin/core/response"
)

// 错误 key 即契约的 PAY_* 错误码（02-openapi-pay-v1.yaml ApiError.errorCode），
// 文案单住 core/i18n/locales；HTTP 状态码沿用框架 response 口径。
const (
	errBizRequired         = "pay.PAY_INVALID_AMOUNT"
	errInvalidAmount       = "pay.PAY_INVALID_AMOUNT"
	errCurrencyUnsupported = "pay.PAY_CURRENCY_UNSUPPORTED"
	errFxRateMissing       = "pay.PAY_FX_RATE_MISSING"
	errStateConflict       = "pay.PAY_STATE_CONFLICT"
	errExpired             = "pay.PAY_EXPIRED"
	errRefundExceeds       = "pay.PAY_REFUND_EXCEEDS"
	errSignInvalid         = "pay.PAY_SIGN_INVALID"
	errChannelUnavailable  = "pay.PAY_CHANNEL_UNAVAILABLE"
	errChannelError        = "pay.PAY_CHANNEL_ERROR"
	errNotFound            = "pay.PAY_NOT_FOUND"
	errTenantMismatch      = "pay.PAY_TENANT_MISMATCH"
	errIdempotencyConflict = "pay.PAY_IDEMPOTENCY_CONFLICT"
	errInternal            = "pay.PAY_INTERNAL"
	// errCapabilityUnsupported 是渠道能力缺口（不支持退款查询/预授权/账单/代付/该币种
	// 或该下单方式）。必须与「渠道拒绝了这笔操作」分开：前者是配置与产品签约问题，
	// 用户要看的是「换渠道或去渠道侧签约」，后者才是业务重试。
	errCapabilityUnsupported = "pay.PAY_CAPABILITY_UNSUPPORTED"
	// errPayoutSettlementTaken 是「这张结算单已经进过别的打款批次」。兜底的是
	// uk_pay_payout_item_settlement，不是应用层预检：预检在并发下先放行再去撞索引，
	// 结论一样，但调用方拿到的是驱动原文。折成 409 + 本码，原错误 Wrap 进日志留证。
	errPayoutSettlementTaken = "pay.PAY_PAYOUT_SETTLEMENT_TAKEN"
	// errNotifyStaleKey 对应哨兵错误 errNotifyStale（见下方 var）：Provider 报
	// 「回调业务时间超前本地时钟」，HandleCallback 把它回成这个 i18n key。
	errNotifyStaleKey = "pay.PAY_NOTIFY_STALE"
	// errDateInvalid 是列表/导出的日期范围格式错误（yyyy-mm-dd）。显式报错而不是
	// 静默忽略：财务按天对账，一个手滑的日期少筛一天就是要出事的数据。
	errDateInvalid            = "pay.PAY_DATE_INVALID"
	errExportTooMany          = "pay.PAY_EXPORT_TOO_MANY"
	// errSettlementTooMany 是结算汇总的行数超限（账期内明细 > settlementMaxRows）。
	// 结算金额必须全量，超限报错让财务缩小账期，绝不静默截断少算。
	errSettlementTooMany      = "pay.PAY_SETTLEMENT_TOO_MANY"
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
	// ResultStale 是回调业务时间超前于本地时钟（伪造或时钟漂移），拒不入账。
	ResultStale = "stale_notify"
	// ResultIgnoredStatePrefix 后面拼资金单状态，例如 ignored_state_paid。
	ResultIgnoredStatePrefix = "ignored_state_"
)

// 抽佣规则状态。
const (
	RuleActive   = "active"
	RuleDisabled = "disabled"
)

var errMockPending = errors.New("mock: pending")

// dupKeyRe 认唯一索引冲突在两引擎上的写法：MySQL/MariaDB 是 1062
// （Duplicate entry ... for key 'uk_x'），PG 是 23505（duplicate key value violates
// unique constraint "uk_x"）。GORM 未开 TranslateError，拿不到 gorm.ErrDuplicatedKey，
// 只能按驱动文本判——所以判定只做「是不是唯一约束拦的」，不猜具体是哪个索引。
var dupKeyRe = regexp.MustCompile(`(?i)duplicate entry|error 1062|duplicate key value|unique constraint|sqlstate 23505`)

// isDuplicateKeyErr 判底层错误是否由唯一索引拦下。调用方据此折成契约 409，
// 并把原错误 Wrap 进去：索引名是「谁拦的」唯一取证，不能丢。
func isDuplicateKeyErr(err error) bool {
	return err != nil && dupKeyRe.MatchString(err.Error())
}

// errNotifyStale 是「回调携带的业务时间超前本地时钟超过允许窗口」的哨兵：
// Provider 在 VerifyCallback 里用 fmt.Errorf("...: %w", errNotifyStale) 报，
// HandleCallback 用 errors.Is 区分，好把审计结果记成 stale_notify 而不是 sign_invalid，
// 也让排障方向从「渠道伪造通知」转到「服务器时钟或渠道时间配置」。
var errNotifyStale = errors.New("pay: notify timestamp outside allowed window")

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
