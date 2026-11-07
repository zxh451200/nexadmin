package auth

// 验证码运营指标：签发 / 校验通过 / 校验失败 / 升级触发，四组原子计数经
// metrics.AddCollector 挂进 /metrics 暴露。没有直方图、没有按 IP 维度——
// 标签基数必须可控（见 core/metrics 对 cardinality 的测试），运营要的答案是
// 「哪一档在被打、通过率多少」，不是一个 IP 列表。

import (
	"sync/atomic"

	"nexadmin/core/metrics"
)

// 验证码形态序号（数组下标），与 captchaKindNames 一一对应。
const (
	captchaKindImage  = 0
	captchaKindSlider = 1
	captchaKindClick  = 2
	captchaKindCount  = 3
)

var captchaKindNames = [captchaKindCount]string{"image", "slider", "click"}

var (
	captchaIssued      [captchaKindCount]atomic.Uint64 // 各形态出题次数
	captchaVerifyPass  [captchaKindCount]atomic.Uint64 // 各形态校验通过
	captchaVerifyFail  [captchaKindCount]atomic.Uint64 // 各形态校验失败（不含 key 缺失/过期这类无效请求）
	captchaEscalations [2]atomic.Uint64                // 升级触发：0=滑块（软阈值） 1=点选（硬阈值）
)

func captchaIssue(kind int)    { captchaIssued[kind].Add(1) }
func captchaPass(kind int)     { captchaVerifyPass[kind].Add(1) }
func captchaFail(kind int)     { captchaVerifyFail[kind].Add(1) }
func captchaEscalate(hard bool) {
	if hard {
		captchaEscalations[1].Add(1)
		return
	}
	captchaEscalations[0].Add(1)
}

// init 把验证码计数挂进 metrics 注册表：Collector 面板里读原子计数渲染文本格式，
// 与 core/metrics 其它 collector（runtime、db stats）同一套机制。
func init() {
	metrics.AddCollector("auth_captcha", func(w *metrics.Writer) {
		for kind, name := range captchaKindNames {
			w.Counter("nexadmin_auth_captcha_issued_total", "Captcha challenges issued.", float64(captchaIssued[kind].Load()), "type", name)
			w.Counter("nexadmin_auth_captcha_verify_pass_total", "Captcha verifications passed.", float64(captchaVerifyPass[kind].Load()), "type", name)
			w.Counter("nexadmin_auth_captcha_verify_fail_total", "Captcha verifications failed.", float64(captchaVerifyFail[kind].Load()), "type", name)
		}
		w.Counter("nexadmin_auth_captcha_escalation_total", "Captcha escalations triggered by login failures.", float64(captchaEscalations[0].Load()), "level", "slider")
		w.Counter("nexadmin_auth_captcha_escalation_total", "Captcha escalations triggered by login failures.", float64(captchaEscalations[1].Load()), "level", "click")
	})
}
