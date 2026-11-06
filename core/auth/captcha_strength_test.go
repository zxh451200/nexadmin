package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	mrand "math/rand/v2"
	"strings"
	"testing"
)

// 这把攻击来自 2026-11-02 的一次实测：当时 core/auth 用固定的 5x7 点阵自绘数字，
// 一个百来行的模板匹配脚本（无 OCR、无训练）把 300 张样本整图解出 100%。
// 这里把同一把尺留在仓库里当回归闸：
//   - 正控制 legacyRender 必须被解开（证明尺子还有效，不是空转绿）；
//   - 生产渲染器必须解不开（谁把它换回字形固定的实现，这里立刻变红）。

// legacyFont / legacyRender 是被替换掉的旧实现，只作为攻击的靶子保留。
var legacyFont = [10][7]uint8{
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

func legacyLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx, dy := absInt(x1-x0), -absInt(y1-y0)
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

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// legacyRender 逐像素复刻替换前的出图逻辑（120x40、4 倍放大、干扰线、噪点）。
// 长度按 captchaLen 走，好让它和现在的攻击面保持同一形状。
func legacyRender(text string) []byte {
	w, h, scale, cell := 30*len(text), 40, 4, 30
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{238, 242, 248, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	for i, ch := range text {
		g := legacyFont[ch-'0']
		ox := i*cell + 4 + mrand.IntN(5)
		oy := 3 + mrand.IntN(5)
		fg := color.RGBA{uint8(20 + mrand.IntN(90)), uint8(30 + mrand.IntN(90)), uint8(80 + mrand.IntN(120)), 255}
		shear := mrand.IntN(5) - 2
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if g[row]&(1<<uint(4-col)) == 0 {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						x := ox + col*scale + dx + truncDiv(row*shear, 3)
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
		legacyLine(img, mrand.IntN(w), mrand.IntN(h), mrand.IntN(w), mrand.IntN(h), c)
	}
	for i := 0; i < 60; i++ {
		img.SetRGBA(mrand.IntN(w), mrand.IntN(h), color.RGBA{uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func truncDiv(a, b int) int {
	q := absInt(a) / absInt(b)
	if (a < 0) != (b < 0) {
		return -q
	}
	return q
}

// dominantColor 找出出现最多的颜色，当作背景——攻击者不会硬编码我们当年的底色。
func dominantColor(img *image.RGBA) color.RGBA {
	counts := map[color.RGBA]int{}
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			counts[img.RGBAAt(x, y)]++
		}
	}
	var best color.RGBA
	bestN := -1
	for c, n := range counts {
		if n > bestN {
			best, bestN = c, n
		}
	}
	return best
}

// crackByLegacyTemplate 用旧的 5x7 点阵模板去匹配任意图：自适应取背景、多档缩放、逐字倾斜。
// 它对"字形固定"的实现有效（正控制），对带扭曲的真字体渲染应当完全失效。
func crackByLegacyTemplate(raw []byte, length int) string {
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Rect, src, bounds.Min, draw.Src)

	bg := dominantColor(img)
	// 先把墨点摊成一张位图：模板匹配要重复读几百万次像素。
	ink := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			diff := absInt(int(c.R)-int(bg.R)) + absInt(int(c.G)-int(bg.G)) + absInt(int(c.B)-int(bg.B))
			ink[y*w+x] = diff > 90
		}
	}
	cell := w / length
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		bestScore := -1.0
		bestDigit := byte(0)
		for _, scale := range []int{3, 4, 5, 6} {
			if 7*scale > h || 5*scale > cell {
				continue
			}
			for ox := i * cell; ox <= i*cell+cell-5*scale && ox <= i*cell+12; ox++ {
				for oy := 1; oy <= 12 && oy+7*scale <= h; oy++ {
					for shear := -2; shear <= 2; shear++ {
						for d := 0; d < 10; d++ {
							s := templateScore(ink, w, h, ox, oy, scale, shear, legacyFont[d])
							if s > bestScore {
								bestScore, bestDigit = s, byte('0'+d)
							}
						}
					}
				}
			}
		}
		out[i] = bestDigit
	}
	return string(out)
}

func templateScore(ink []bool, w, h, ox, oy, scale, shear int, glyph [7]uint8) float64 {
	var same, total float64
	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			want := (glyph[row] >> uint(4-col)) & 1
			hit, seen := 0, 0
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					x := ox + col*scale + dx + truncDiv(row*shear, 3)
					y := oy + row*scale + dy
					if x >= 0 && x < w && y >= 0 && y < h {
						seen++
						if ink[y*w+x] {
							hit++
						}
					}
				}
			}
			got := 0
			if seen > 0 && float64(hit)/float64(seen) >= 0.5 {
				got = 1
			}
			total++
			if float64(got) == float64(want) {
				same++
			}
		}
	}
	return same / total
}

func decodeDataURI(t *testing.T, uri string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// 正控制：这把攻击必须能整片解开旧的点阵实现，否则下面的断言是空转。
func TestLegacyTemplateAttackCracksOldBitmapRenderer(t *testing.T) {
	const samples = 20
	cracked := 0
	for i := 0; i < samples; i++ {
		text, err := randDigits(CaptchaLenDefault)
		if err != nil {
			t.Fatal(err)
		}
		if crackByLegacyTemplate(legacyRender(text), CaptchaLenDefault) == text {
			cracked++
		}
	}
	if cracked*10 < samples*9 { // 至少 90%
		t.Fatalf("攻击对旧点阵实现只解出 %d/%d，尺子失效，下面的强度断言不成立", cracked, samples)
	}
}

// 生产渲染器必须挡住同一把攻击。整图 5 个位置全猜中的偶然概率约 10^-5，所以一张都不许被解开。
func TestCaptchaResistsLegacyTemplateAttack(t *testing.T) {
	s := newCaptchaSvc(t)
	ctx := context.Background()
	const samples = 10
	cracked := 0
	for i := 0; i < samples; i++ {
		key, uri, err := s.NewCaptcha(ctx, testIP)
		if err != nil {
			t.Fatal(err)
		}
		text := answerOf(t, s, key)
		if crackByLegacyTemplate(decodeDataURI(t, uri), CaptchaLenDefault) == text {
			t.Logf("key %s 被模板匹配解出：%s", key, text)
			cracked++
		}
	}
	if cracked > 0 {
		t.Fatalf("%d/%d 张验证码被固定模板解出，渲染层退化成了字形固定的实现", cracked, samples)
	}
}
