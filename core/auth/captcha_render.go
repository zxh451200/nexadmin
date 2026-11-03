package auth

import (
	"bytes"
	"image/color"
	"strings"

	"github.com/dchest/captcha"
)

// 出图尺寸取登录框（112x36 CSS 像素）的 2 倍：再大就会在缩到显示尺寸时糊成一团，
// dchest 自己的 240x80 是按 6 位数字调的，5 位时数字偏大、下采样后反而难读。
const (
	capW, capH = 224, 72
)

// capBackground 是强制的浅色底。dchest 默认把背景留成透明、数字画成 0-128 的深色，
// 直接放到深色主题下就是"黑底黑字"，人眼也读不出来。
var capBackground = color.RGBA{238, 242, 248, 255}

// capInk 是数字（调色板 1 号位）的取色上限：保证与浅色底之间有足够对比。
const capInkMax = 128

// digitBytes 把答案 "12345" 转成渲染器要的数值字节 [1 2 3 4 5]。
func digitBytes(text string) []byte {
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if c := text[i]; c >= '0' && c <= '9' {
			out = append(out, c-'0')
		}
	}
	return out
}

// renderCaptcha 出 PNG。key 只用于给渲染器的随机源定种，同一张验证码重绘结果稳定。
func renderCaptcha(key, text string) ([]byte, error) {
	img := captcha.NewImage(key, digitBytes(text), capW, capH)
	fixPalette(img.Palette)
	var buf bytes.Buffer
	if _, err := img.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fixPalette 改写渲染器的调色板：0 号位（它当作透明背景）换成不透明浅色，
// 1 号位（数字）压暗到浅底之下足够对比；干扰圆沿用随机色，落在浅底上自然变淡。
func fixPalette(pal color.Palette) {
	if len(pal) < 2 {
		return
	}
	pal[0] = capBackground
	prim := color.NRGBAModel.Convert(pal[1]).(color.NRGBA)
	sum := int(prim.R) + int(prim.G) + int(prim.B)
	if sum > capInkMax*3 {
		pal[1] = color.RGBA{
			uint8(int(prim.R) * capInkMax * 3 / sum),
			uint8(int(prim.G) * capInkMax * 3 / sum),
			uint8(int(prim.B) * capInkMax * 3 / sum),
			255,
		}
	}
}

// renderCaptchaAudio 出 WAV，供音频验证码使用。
func renderCaptchaAudio(key, text, lang string) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := captcha.NewAudio(key, digitBytes(text), audioLang(lang)).WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// audioLang 把界面语言映射到渲染器内置的语音包（en/ja/ru/zh/pt），其余回落英文。
func audioLang(lang string) string {
	prefix := lang
	if i := strings.IndexByte(lang, '-'); i >= 0 {
		prefix = strings.ToLower(lang[:i])
	}
	switch prefix {
	case "zh", "en", "ja", "ru", "pt":
		return prefix
	}
	return "en"
}
