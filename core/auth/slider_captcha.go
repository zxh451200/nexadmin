package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/cache"
)

// 滑块验证码：与图形验证码并存的第二种人机校验形态（对标国内产品的滑块拼图体验）。
//
// 流程：NewSliderCaptcha 生成拼图（背景挖孔 + 滑块图），孔位 X 只有服务端知道；
// 前端让用户把滑块拖到孔位，拖动过程记录轨迹；VerifySliderCaptcha 校验
// 「落点与孔位的容差」+「轨迹像人」（时长、点数、单调推进），通过后把一次性 token
// 写到图形验证码同一个键（captchaKey）下，前端拿它当 captcha 字段连同 captchaKey 提交，
// 于是登录、短信、注册三条链路走完全相同的 VerifyCaptcha 通道（IP 绑定、GetDel 一次性消费），
// 登录侧零改动。
//
// 防爆破：verify 每个 key 最多 5 次尝试（超过即作废整个挑战），token 60 秒内有效，
// 签发与消费都绑定 IP。

const (
	sliderTTL       = 120 * time.Second // 拖拽比输密码慢，给 2 分钟
	sliderTriesMax  = 5                 // 单个挑战最多校验 5 次
	sliderTolerance = 6                 // 落点与孔位的容差（px，前后端同尺寸）
	sliderTrailMin  = 6                 // 轨迹点数下限
	sliderDurMin    = 150 * time.Millisecond
	sliderDurMax    = 15 * time.Second
)

// errSliderUnavailable 表示挑战不存在/已过期/已被消费。
var errSliderUnavailable = fmt.Errorf("slider captcha unavailable")

// errSliderFailed 表示校验未通过（落点偏了或轨迹不像人）。
var errSliderFailed = fmt.Errorf("slider captcha failed")

func sliderKey(key string) string { return cache.GlobalKey("auth", "slider", key) }
func sliderTries(k string) string { return cache.GlobalKey("auth", "slider-tries", k) }

// NewSliderCaptcha 生成滑块挑战：返回 key、背景图 dataURI、滑块图 dataURI 与孔位 Y。
// X（孔位横坐标）只留在服务端。
func (s *Service) NewSliderCaptcha(ctx context.Context, ip string) (key, bg, piece string, pieceY int, err error) {
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", 0, err
	}
	key = hex.EncodeToString(raw)
	bgPNG, piecePNG, secretX, pieceY, rerr := renderSlider(raw)
	if rerr != nil {
		return "", "", "", 0, rerr
	}
	if err = s.cache.Set(ctx, sliderKey(key), ip+"|"+strconv.Itoa(secretX), sliderTTL); err != nil {
		return "", "", "", 0, err
	}
	return key, "data:image/png;base64," + base64.StdEncoding.EncodeToString(bgPNG),
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(piecePNG), pieceY, nil
}

// trailPoint 是前端上报的一个轨迹采样（相对页面加载起点的毫秒时间与滑块横坐标）。
type trailPoint struct {
	T int64 `json:"t"`
	X int   `json:"x"`
}

// sliderSecret 从 cache 取挑战的 ip 与孔位 X；不存在/过期报 errSliderUnavailable。
func (s *Service) sliderSecret(ctx context.Context, key string) (ip string, x int, err error) {
	v, gerr := s.cache.Get(ctx, sliderKey(key))
	if gerr != nil {
		return "", 0, errSliderUnavailable
	}
	boundIP, xs, found := strings.Cut(v, "|")
	if !found {
		return "", 0, errSliderUnavailable
	}
	x, cerr := strconv.Atoi(xs)
	if cerr != nil {
		return "", 0, errSliderUnavailable
	}
	return boundIP, x, nil
}

// VerifySliderCaptcha 校验滑块挑战：通过则签发一次性 token（60 秒），登录时作为
// captcha 提交；未通过返回 errSliderFailed（失败不消费挑战，但累计尝试次数）。
// 签发后挑战即被消费（重复 verify 同一挑战报不可用），防在同一孔位上反复试错。
func (s *Service) VerifySliderCaptcha(ctx context.Context, key string, x int, trail []trailPoint, ip string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 {
		return "", errSliderUnavailable
	}
	// 尝试次数：超过上限即作废整个挑战（防在同一挑战上无限暴力试 X）。
	tries, err := cache.Incr(s.cache, ctx, sliderTries(key), sliderTTL)
	if err != nil {
		return "", err
	}
	if tries > sliderTriesMax {
		_ = s.cache.Del(ctx, sliderKey(key))
		return "", errSliderUnavailable
	}
	ipBound, secretX, err := s.sliderSecret(ctx, key)
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(ipBound), []byte(ip)) != 1 {
		return "", errSliderUnavailable
	}
	if !trailLooksHuman(trail, x, secretX) {
		return "", errSliderFailed
	}
	// 签发一次性 token：写到图形验证码同一个键下（captchaKey），登录侧才能用
	// VerifyCaptcha 原样消费；挑战本身作废，避免同一 key 二次拖拽。
	tok := make([]byte, 16)
	if _, err = rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	if err = s.cache.Set(ctx, captchaKey(key), ip+"|"+token, captchaTTL); err != nil {
		return "", err
	}
	_ = s.cache.Del(ctx, sliderKey(key))
	return token, nil
}

// trailLooksHuman 做轻量行为校验：时长/点数/推进量达标、时间戳单调。
// 它挡不住写好的自动化脚本（那靠 rate limit 与一次性消费兜底），挡的是"瞬间位移"这种零成本攻击。
func trailLooksHuman(trail []trailPoint, finalX, secretX int) bool {
	if len(trail) < sliderTrailMin || len(trail) > 4096 {
		return false
	}
	first, last := trail[0], trail[len(trail)-1]
	dur := time.Duration(last.T-first.T) * time.Millisecond
	if dur < sliderDurMin || dur > sliderDurMax {
		return false
	}
	if last.X != finalX || last.X < 0 {
		return false
	}
	minX, maxX := last.X, last.X
	prevT := first.T
	for i, p := range trail {
		if i > 0 && p.T < prevT {
			return false // 时间戳必须单调
		}
		prevT = p.T
		minX = min(minX, p.X)
		maxX = max(maxX, p.X)
	}
	// 拖动要有实际推进量，且落点在容差内。
	if maxX-minX < 40 {
		return false
	}
	dx := finalX - secretX
	if dx < 0 {
		dx = -dx
	}
	return dx <= sliderTolerance
}
