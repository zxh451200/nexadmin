package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	mrand "math/rand/v2"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/cache"
)

// 图形验证码：数字 + 干扰线，PNG base64 返回；答案存 core/cache，60 秒有效、一次性（校验一次即作废，无论对错）。
// 登录失败计数同样放在 core/cache，按“用户名”和“IP”两个维度各记一份，任一维度连续失败 >= captchaAfterFails 次就要求验证码。
// 计数只看提交的用户名字符串，与账号是否存在无关，因此不会泄露账号是否存在。

const (
	captchaLen        = 4
	captchaAfterFails = 3
	failWindow        = 15 * time.Minute

	capW, capH = 120, 40
)

// captchaTTL 是验证码有效期（var 仅为让单测能缩短）。
var captchaTTL = 60 * time.Second

func captchaKey(key string) string { return cache.GlobalKey("auth", "captcha", key) }

func failUserKey(username string) string {
	return cache.GlobalKey("auth", "loginfail", "u", strings.ToLower(strings.TrimSpace(username)))
}

func failIPKey(ip string) string { return cache.GlobalKey("auth", "loginfail", "ip", ip) }

// 5x7 点阵数字，每行 5 位。
var digitFont = [10][7]uint8{
	{0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	{0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	{0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111},
	{0b11110, 0b00001, 0b00001, 0b01110, 0b00001, 0b00001, 0b11110},
	{0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	{0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110},
	{0b00110, 0b01000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110},
	{0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	{0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	{0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100},
}

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

func line(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(img.Rect) {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// renderCaptcha 用标准库 image 自绘：浅色底 + 逐字抖动/倾斜的数字 + 干扰线 + 噪点。
func renderCaptcha(text string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, capW, capH))
	bg := color.RGBA{238, 242, 248, 255}
	for y := 0; y < capH; y++ {
		for x := 0; x < capW; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	scale := 4
	cell := capW / captchaLen
	for i, ch := range text {
		g := digitFont[ch-'0']
		ox := i*cell + 4 + mrand.IntN(5)
		oy := 3 + mrand.IntN(5)
		fg := color.RGBA{uint8(20 + mrand.IntN(90)), uint8(30 + mrand.IntN(90)), uint8(80 + mrand.IntN(120)), 255}
		shear := mrand.IntN(5) - 2 // 每行向右偏移的 1/3 像素量，形成倾斜
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if g[row]&(1<<uint(4-col)) == 0 {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						x := ox + col*scale + dx + (row*shear)/3
						y := oy + row*scale + dy
						if image.Pt(x, y).In(img.Rect) {
							img.SetRGBA(x, y, fg)
						}
					}
				}
			}
		}
	}
	for i := 0; i < 5; i++ {
		c := color.RGBA{uint8(90 + mrand.IntN(100)), uint8(100 + mrand.IntN(100)), uint8(120 + mrand.IntN(100)), 255}
		line(img, mrand.IntN(capW), mrand.IntN(capH), mrand.IntN(capW), mrand.IntN(capH), c)
	}
	for i := 0; i < 60; i++ {
		img.SetRGBA(mrand.IntN(capW), mrand.IntN(capH), color.RGBA{uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// NewCaptcha 生成验证码，返回 key 与 data URI 图片；答案 60 秒后失效。
// 答案与签发时的客户端 IP 绑定存储（ip|answer），校验时必须来自同一 IP，防止 key+答案被转交给其他来源使用。
func (s *Service) NewCaptcha(ctx context.Context, ip string) (key, dataURI string, err error) {
	text, err := randDigits(captchaLen)
	if err != nil {
		return "", "", err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	key = hex.EncodeToString(raw)
	pngBytes, err := renderCaptcha(text)
	if err != nil {
		return "", "", err
	}
	if err := s.cache.Set(ctx, captchaKey(key), ip+"|"+text, captchaTTL); err != nil {
		return "", "", err
	}
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
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(wantText), []byte(answer)) == 1, nil
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
