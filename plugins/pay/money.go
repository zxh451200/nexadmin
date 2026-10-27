package pay

import (
	"math/big"

	"nexadmin/core/response"
)

// 金额与舍入规范（02-openapi-pay-v1.yaml）：
//   - 一切金额 int64 最小货币单位（*_minor），禁止 float；
//   - 比率用基点（1bp = 0.01%），汇率用 1e-6 定点（fxRateMicro）；
//   - 基点计算与跨行分摊 half-even（banker's rounding），单据行舍入 half-up。
//     本插件只产出费率/汇率类计算，统一 half-even；单据行由业务侧按 half-up 落库。
const (
	bpDenominator    = 10000
	microDenominator = 1000000
	// FxUnity 是同币种汇率的定点值：1.000000。
	FxUnity = microDenominator
)

var (
	bpDen   = big.NewInt(bpDenominator)
	microDe = big.NewInt(microDenominator)
	one     = big.NewInt(1)
)

// mulRoundHalfEven 计算 n*d/m（m>0），half-even 舍入，全程 big.Int 不溢出。
func mulRoundHalfEven(n, d *big.Int, m *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(new(big.Int).Mul(n, d), m, new(big.Int))
	// QuoRem 向零截断，r 与 n*d 同号；half-even 只看 |r| 与 m/2、m 的关系。
	r.Abs(r)
	twice := new(big.Int).Lsh(r, 1)
	switch twice.Cmp(m) {
	case 1: // |r| > m/2：远离零进位
		if q.Sign() < 0 {
			q.Sub(q, one)
		} else {
			q.Add(q, one)
		}
	case 0: // 恰为半：舍入到偶数
		if q.Bit(0) == 1 {
			if q.Sign() < 0 {
				q.Sub(q, one)
			} else {
				q.Add(q, one)
			}
		}
	}
	return q
}

// MulBp 计算 amount × bp / 10000，half-even 舍入。amount 与 bp 均可为负。
func MulBp(amount, bp int64) int64 {
	return mulRoundHalfEven(big.NewInt(amount), big.NewInt(bp), bpDen).Int64()
}

// FxConvert 把外币 minor 金额按 fxRateMicro（1e-6 定点）折算成币种 minor 金额，half-even 舍入。
func FxConvert(amountMinor, fxRateMicro int64) int64 {
	return mulRoundHalfEven(big.NewInt(amountMinor), big.NewInt(fxRateMicro), microDe).Int64()
}

// feeOf 按「主体精确 > 租户默认」的规则优先级计算手续费：
// feeMinor = MulBp(amountMinor, rateBp) + fixedMinor。rate 为 nil 表示无规则，手续费 0。
func feeOf(amountMinor int64, rule *CommissionRule) int64 {
	if rule == nil {
		return 0
	}
	return MulBp(amountMinor, rule.RateBp) + rule.FixedMinor
}

// feeRefundOf 计算退款应退回的手续费，与退款金额同舍入口径，且不超过已收手续费。
func feeRefundOf(refundAmountMinor int64, rule *CommissionRule, feeMinor int64) int64 {
	f := feeOf(refundAmountMinor, rule)
	if f > feeMinor {
		return feeMinor
	}
	return f
}

const (
	fxBad = "pay.PAY_FX_RATE_MISSING"
)

// normalizeFx 归一汇率：外币单缺汇率报 PAY_FX_RATE_MISSING；同币种缺省 1.000000。
// 返回 (fxRateMicro, amountOrigMinor, origCurrency, err)。
func normalizeFx(fxRateMicro int64, amountOrigMinor int64, origCurrency, currency string) (int64, int64, string, error) {
	if origCurrency == "" || origCurrency == currency {
		if amountOrigMinor != 0 && origCurrency != "" {
			return 0, 0, "", response.BadRequest(fxBad)
		}
		return FxUnity, 0, "", nil
	}
	if fxRateMicro <= 0 {
		return 0, 0, "", response.BadRequest(fxBad)
	}
	return fxRateMicro, amountOrigMinor, origCurrency, nil
}
