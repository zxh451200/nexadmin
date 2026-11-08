package auth

// 点选验证码：第三档人机校验——同一用户名或 IP 连错达到硬阈值（hardAfterFails）后，
// 登录失败响应的 reason 从 CAPTCHA_REQUIRED 升级为 CAPTCHA_REQUIRED_HARD，前端把
// 默认形态从滑块换成点选。点选对「专攻滑块拖拽」的自动化是一层形式级筛选：目标文字
// 每次随机重排，没有可拖拽的把手可模拟。
//
// 流程与滑块同构（复用同一套 cache 键约定与 token 通道）：
//   NewClickCaptcha 出题 → 图片 + 候选字条 + 点位（只存服务端）；
//   VerifyClickCaptcha 按提交顺序逐点比对（点位容差 = 字块半径 + 2px），通过后把
//   一次性 token 写进 captchaKey，登录/短信/注册的 VerifyCaptcha 原样消费。
// 防爆破与滑块同口径：TTL 120s、单挑战最多 5 次校验、IP 绑定。
//
// 渲染走 go-captcha v2 click 包（与滑块同一家库）：字体为 captchafonts/ 内嵌的
// 思源黑体子集（OFL 许可，商用可再分发——官方素材包的方正字体商用需授权，不能
// 随产品分发），字符池来自 captchafonts/charset.txt（约 200 字，远大于库默认的
// 15 字，猜字空间才有意义）。字体解码失败时生成报错——点选是升级路径，不像滑块
// 是默认路径，没有手绘回退的必要（上层拿错误照常回 5xx，登录入口不受影响）。

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/subtle"
	_ "embed" // //go:embed 读取 string/[]byte 变量需要空白导入
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/freetype/truetype"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
	"nexadmin/core/cache"
)

//go:embed captchafonts/clickfont.ttf
var clickFontTTF []byte

//go:embed captchafonts/charset.txt
var clickCharsetTxt string

// clickCharset 从 charset.txt 读字符池：跳过注释行与空行，逐字去重保序。
// 该文件同时是字体子集（gen_subset.py）与字形覆盖测试的唯一来源。
func clickCharset() []string {
	seen := make(map[rune]bool, 256)
	var chars []string
	for _, line := range strings.Split(clickCharsetTxt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, r := range line {
			if !seen[r] {
				seen[r] = true
				chars = append(chars, string(r))
			}
		}
	}
	return chars
}

const (
	clickTTL      = 120 * time.Second // 点选比拖拽还慢，给 2 分钟
	clickTriesMax = 5                 // 单个挑战最多校验 5 次
	clickThumbW   = 150               // 候选字条尺寸（与 builder 的 WithRangeThumbImageSize 同口径）
	clickThumbH   = 40
	// clickImgH 是点选主图高度：比滑块面板（150）高 20px——大字号 + ±25° 旋转的字形
	// 需要纵向余量，贴着 150 的下边缘会把字裁掉半截（实测踩过）。宽度仍与滑块通栏对齐。
	clickImgH = 170
)

var errClickUnavailable = errors.New("click captcha unavailable")
var errClickFailed = errors.New("click captcha failed")

func clickKey(key string) string { return cache.GlobalKey("auth", "click", key) }
func clickTries(key string) string { return cache.GlobalKey("auth", "click-tries", key) }

var (
	clickOnce  sync.Once
	clickCap   click.Captcha
	clickErr   error
	clickFontW int // 候选字条宽（builder 生成的实际尺寸，随响应下发）
	clickFontH int
)

// clickCaptcha 惰性构建点选验证码：字体解析只做一次，全进程复用同一个 builder。
// 画布与滑块同宽（384×150，通栏对齐）；主图 3~4 个字、要求点选其中 2~3 个。
// 可读性四件套（实测 384×150 + 库默认参数时字几乎认不出）：
//   - 标准印刷体（思源黑体 Medium）而非艺术字，花底上也能一眼认出；
//   - 字号放大到 30~38，倾斜限制在 ±25°（库默认最大到 60°，转过头就认不出了）；
//   - 干扰字压到 0~1 个（画 3~4 个字里点 2~3 个），找字的搜索面积减半；
//   - 高亮字色 + 深色投影：库默认调色板全是马卡龙淡色，在彩色照片底上直接融掉
//     （实测「名」和背景黄球一个亮度）——白/亮黄/亮绿等高亮色加 3px 深投影，
//     字在任意底上都浮起来。
func clickCaptcha() (click.Captcha, error) {
	clickOnce.Do(func() {
		ttf, err := truetype.Parse(clickFontTTF)
		if err != nil {
			clickErr = err
			return
		}
		bgs, err := sharedPhotoBackgrounds()
		if err != nil {
			clickErr = err
			return
		}
		b := click.NewBuilder(
			click.WithImageSize(option.Size{Width: sliderBgW, Height: clickImgH}),
			click.WithRangeLen(option.RangeVal{Min: 3, Max: 4}),
			click.WithRangeSize(option.RangeVal{Min: 30, Max: 38}),
			click.WithRangeAnglePos([]option.RangeVal{{Min: -25, Max: 25}}),
			click.WithRangeVerifyLen(option.RangeVal{Min: 2, Max: 3}),
			click.WithRangeThumbSize(option.RangeVal{Min: 26, Max: 32}),
			// 候选字条去干扰：波浪线与圆点会压在字上（实测「子」被线穿成认不出），
			// 干净的浅底 + 彩色字才是「照着找」的正确形态
			click.WithRangeThumbBgDistort(0),
			click.WithRangeThumbBgCirclesNum(0),
			click.WithRangeThumbBgSlimLineNum(0),
			click.WithRangeThumbImageSize(option.Size{Width: clickThumbW, Height: clickThumbH}),
			// 高亮字色 + 深投影：见上方可读性第四件
			click.WithRangeColors([]string{
				"#ffffff", // 白：任意底上的万能高对比
				"#ffde59", // 亮黄
				"#7dff8a", // 亮绿
				"#ffb14d", // 亮橙
				"#6fe3ff", // 亮青
			}),
			click.WithDisplayShadow(true),
			click.WithShadowColor("#111318"),
			click.WithShadowPoint(option.Point{X: 3, Y: 3}),
		)
		b.SetResources(click.WithFonts([]*truetype.Font{ttf}), click.WithChars(clickCharset()), click.WithBackgrounds(bgs))
		clickCap = b.Make()
		clickFontW, clickFontH = clickThumbW, clickThumbH
	})
	return clickCap, clickErr
}

// clickDot 是一个期望点击点：X/Y 为字块中心附近的落点口径（字块包围盒），Size 是字块边长。
type clickDot struct {
	X    int `json:"x"`
	Y    int `json:"y"`
	Size int `json:"size"`
}

// clickPoint 是前端提交的一个点击点（图片像素坐标）。
type clickPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// NewClickCaptcha 出一张点选挑战：主图（混入干扰字）、候选字条（要求按顺序点选的字）、
// 点位（只存服务端）。返回的尺寸常量给前端按 1:1 摆放图片。
func (s *Service) NewClickCaptcha(ctx context.Context, ip string) (key, image, thumb string, w, h, tw, th int, err error) {
	capt, gerr := clickCaptcha()
	if gerr != nil {
		return "", "", "", 0, 0, 0, 0, gerr
	}
	data, gerr := capt.Generate()
	if gerr != nil {
		return "", "", "", 0, 0, 0, 0, gerr
	}
	dots := data.GetData()
	if len(dots) == 0 {
		return "", "", "", 0, 0, 0, 0, fmt.Errorf("click captcha: empty dots")
	}
	// map[int]*Dot 按 Index 升序即期望点击顺序（Index 是绘制序，候选字条同序）。
	idx := make([]int, 0, len(dots))
	for i := range dots {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	ordered := make([]clickDot, 0, len(dots))
	for _, i := range idx {
		d := dots[i]
		ordered = append(ordered, clickDot{X: d.X, Y: d.Y, Size: d.Size})
	}
	raw := make([]byte, 16)
	if _, err = crand.Read(raw); err != nil {
		return "", "", "", 0, 0, 0, 0, err
	}
	key = hex.EncodeToString(raw)
	payload, err := json.Marshal(ordered)
	if err != nil {
		return "", "", "", 0, 0, 0, 0, err
	}
	if err = s.cache.Set(ctx, clickKey(key), ip+"|"+string(payload), clickTTL); err != nil {
		return "", "", "", 0, 0, 0, 0, err
	}
	captchaIssue(captchaKindClick)
	var buf bytes.Buffer
	if err = png.Encode(&buf, data.GetMasterImage().Get()); err != nil {
		return "", "", "", 0, 0, 0, 0, err
	}
	image = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	buf.Reset()
	if err = png.Encode(&buf, data.GetThumbImage().Get()); err != nil {
		return "", "", "", 0, 0, 0, 0, err
	}
	thumb = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	return key, image, thumb, sliderBgW, clickImgH, clickFontW, clickFontH, nil
}

// VerifyClickCaptcha 按提交顺序校验点位：数目必须与点位数一致，每个点落在对应字块的
// 「半径 + 2px」内。通过则签发一次性 token（写 captchaKey 通道，与滑块/图形码同消费口），
// 挑战作废；失败只计次数不消费挑战，超过 5 次作废整个挑战。
func (s *Service) VerifyClickCaptcha(ctx context.Context, key string, points []clickPoint, ip string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 {
		return "", errClickUnavailable
	}
	tries, err := cache.Incr(s.cache, ctx, clickTries(key), clickTTL)
	if err != nil {
		return "", err
	}
	if tries > clickTriesMax {
		_ = s.cache.Del(ctx, clickKey(key))
		return "", errClickUnavailable
	}
	want, gerr := s.cache.Get(ctx, clickKey(key))
	if gerr != nil {
		return "", errClickUnavailable
	}
	boundIP, payload, found := strings.Cut(want, "|")
	if !found {
		return "", errClickUnavailable
	}
	if subtle.ConstantTimeCompare([]byte(boundIP), []byte(ip)) != 1 {
		return "", errClickUnavailable
	}
	var expected []clickDot
	if err := json.Unmarshal([]byte(payload), &expected); err != nil {
		return "", errClickUnavailable
	}
	if len(points) != len(expected) {
		captchaFail(captchaKindClick)
		return "", errClickFailed
	}
	for i, d := range expected {
		// 容差 = 字块半径 + 3px：字形有 ±25° 倾斜，视觉中心与包围盒中心略有偏移，多留一点余量
		tol := d.Size/2 + 3
		dx, dy := points[i].X-d.X, points[i].Y-d.Y
		if absClick(dx) > tol || absClick(dy) > tol {
			captchaFail(captchaKindClick)
			return "", errClickFailed
		}
	}
	tok := make([]byte, 16)
	if _, err := crand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	if err := s.cache.Set(ctx, captchaKey(key), ip+"|"+token, captchaTTL); err != nil {
		return "", err
	}
	_ = s.cache.Del(ctx, clickKey(key))
	captchaPass(captchaKindClick)
	return token, nil
}

func absClick(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
