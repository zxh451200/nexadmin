package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/cache"
)

// 图形验证码：答案存 core/cache，60 秒有效、一次性（校验一次即作废，无论对错）。
// 出图与出声都在 captcha_render.go：字形渲染带随机缩放/倾斜/波扭曲，
// 早先用固定 5x7 点阵自绘的版本能被百行模板匹配整图解出，见 captcha_strength_test.go。
// 登录失败计数同样放在 core/cache，按“用户名”和“IP”两个维度各记一份，任一维度连续失败 >= captchaAfterFails 次就要求验证码。
// 计数只看提交的用户名字符串，与账号是否存在无关，因此不会泄露账号是否存在。

const (
	// CaptchaLenDefault 是验证码位数的出厂值；实际位数由参数设置 login.captcha_length
	// 控制（运行时经 captchaLengthSource 注入，见 source.go），范围钳在 4–8。
	// 导出给 config 模块当参数出厂值与回退值，两侧同一口径。
	CaptchaLenDefault = 5
	// CaptchaLenMin / CaptchaLenMax 是允许配置的上下限：设置页按它做保存校验，
	// 本包按它做运行时钳制——两处必须同源，否则界面存的值和实际生成的位数会分家。
	CaptchaLenMin = 4
	CaptchaLenMax = 8
	captchaAfterFails = 3
	// hardAfterFails 是「点选验证码」的升级阈值（滑块阈值的两倍）：连错到这个量级说明
	// 来者不是手滑——验证形式本身升级成点选，把专攻滑块的自动化工具一并筛出去。
	hardAfterFails    = captchaAfterFails * 2
	failWindow        = 15 * time.Minute
)

// errCaptchaUnavailable 表示这张验证码取不到内容（不存在、已过期或来源 IP 不符）。
// 三种原因对外合并成一句，避免音频接口变成"探测 key 是否存在"的通道。
var errCaptchaUnavailable = errors.New("captcha unavailable")

// captchaTTL 是验证码有效期（var 仅为让单测能缩短）。
var captchaTTL = 60 * time.Second

func captchaKey(key string) string { return cache.GlobalKey("auth", "captcha", key) }

func failUserKey(username string) string {
	return cache.GlobalKey("auth", "loginfail", "u", strings.ToLower(strings.TrimSpace(username)))
}

func failIPKey(ip string) string { return cache.GlobalKey("auth", "loginfail", "ip", ip) }

func randDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = '0' + v%10
	}
	return string(out), nil
}

// captchaLength 取当前配置的验证码位数（钳在 4–8）；源未注入/取值非法时回落出厂值。
func (s *Service) captchaLength(ctx context.Context) int {
	if fn, ok := captchaLengthSource.Load().(func(context.Context) int); ok && fn != nil {
		if n := fn(ctx); n >= CaptchaLenMin && n <= CaptchaLenMax {
			return n
		}
	}
	return CaptchaLenDefault
}

// NewCaptcha 生成验证码，返回 key 与 data URI 图片；答案 60 秒后失效。
// 答案与签发时的客户端 IP 绑定存储（ip|answer），校验时必须来自同一 IP，防止 key+答案被转交给其他来源使用。
func (s *Service) NewCaptcha(ctx context.Context, ip string) (key, dataURI string, err error) {
	text, err := randDigits(s.captchaLength(ctx))
	if err != nil {
		return "", "", err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	key = hex.EncodeToString(raw)
	pngBytes, err := renderCaptcha(key, text)
	if err != nil {
		return "", "", err
	}
	if err := s.cache.Set(ctx, captchaKey(key), ip+"|"+text, captchaTTL); err != nil {
		return "", "", err
	}
	captchaIssue(captchaKindImage)
	return key, "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), nil
}

// VerifyCaptcha 校验并作废验证码（一次性：无论对错，同一个 key 只能校验一次）。
// ip 与签发时不一致即失败（签发时未带 IP 的记录不绑定，答案为整串值）。
func (s *Service) VerifyCaptcha(ctx context.Context, key, answer, ip string) (bool, error) {
	key, answer = strings.TrimSpace(key), strings.TrimSpace(answer)
	if key == "" || len(key) > 64 {
		return false, nil
	}
	// cache.GetDel 原子消费：多实例并发提交同一个 key，只有一个能取到答案；底层不支持时回落为 Get+Del（带 warn）。
	want, err := cache.GetDel(s.cache, ctx, captchaKey(key))
	if errors.Is(err, cache.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	boundIP, wantText, found := strings.Cut(want, "|")
	if !found {
		boundIP, wantText = "", want
	}
	if boundIP != "" && subtle.ConstantTimeCompare([]byte(boundIP), []byte(ip)) != 1 {
		captchaFail(captchaKindImage)
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(wantText), []byte(answer)) == 1 {
		captchaPass(captchaKindImage)
		return true, nil
	}
	captchaFail(captchaKindImage)
	return false, nil
}

// CaptchaAudio 把同一张图形验证码读成音频（WAV 字节），供视障用户听写。
// 它只读取、不消费：消费仍然只发生在 VerifyCaptcha，所以用户可以反复听、听完再输。
// key 不存在、已过期、或来源 IP 与签发时不符，都返回同一个 errCaptchaUnavailable，避免变成探测 key 的通道。
func (s *Service) CaptchaAudio(ctx context.Context, key, ip, lang string) ([]byte, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 {
		return nil, errCaptchaUnavailable
	}
	want, err := s.cache.Get(ctx, captchaKey(key))
	if errors.Is(err, cache.ErrNotFound) {
		return nil, errCaptchaUnavailable
	}
	if err != nil {
		return nil, err
	}
	boundIP, text, found := strings.Cut(want, "|")
	if !found {
		boundIP, text = "", want
	}
	if boundIP != "" && subtle.ConstantTimeCompare([]byte(boundIP), []byte(ip)) != 1 {
		return nil, errCaptchaUnavailable
	}
	return renderCaptchaAudio(key, text, lang)
}

func (s *Service) failCount(ctx context.Context, key string) (int, error) {
	v, err := s.cache.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(v)
	return n, nil
}

// LoginNeedsCaptcha 报告该用户名或该 IP 是否已连续失败达到阈值。
func (s *Service) LoginNeedsCaptcha(ctx context.Context, username, ip string) (bool, error) {
	for _, k := range []string{failUserKey(username), failIPKey(ip)} {
		n, err := s.failCount(ctx, k)
		if err != nil {
			return false, err
		}
		if n >= captchaAfterFails {
			return true, nil
		}
	}
	return false, nil
}

// LoginNeedsHardCaptcha 报告是否该把验证码升级成点选：同一组计数器、更高的硬阈值。
// 与 LoginNeedsCaptcha 读同一份计数，调用时机在 RecordLoginFailure 之后（计数已含本次）。
func (s *Service) LoginNeedsHardCaptcha(ctx context.Context, username, ip string) (bool, error) {
	for _, k := range []string{failUserKey(username), failIPKey(ip)} {
		n, err := s.failCount(ctx, k)
		if err != nil {
			return false, err
		}
		if n >= hardAfterFails {
			return true, nil
		}
	}
	return false, nil
}

// RecordLoginFailure 记一次登录失败（用户名、IP 各一份），返回之后是否需要验证码。
func (s *Service) RecordLoginFailure(ctx context.Context, username, ip string) (bool, error) {
	need := false
	for _, k := range []string{failUserKey(username), failIPKey(ip)} {
		// 原子 Incr：并发失败不丢计数；窗口从第一次失败开始，固定 failWindow（不再随每次失败顺延）。
		n, err := cache.Incr(s.cache, ctx, k, failWindow)
		if err != nil {
			return false, err
		}
		need = need || n >= captchaAfterFails
	}
	return need, nil
}

// ClearLoginFailures 登录成功后清除该用户名的失败计数。IP 维度不清，免得攻击者靠登录自己的账号重置计数。
func (s *Service) ClearLoginFailures(ctx context.Context, username string) error {
	return s.cache.Del(ctx, failUserKey(username))
}
