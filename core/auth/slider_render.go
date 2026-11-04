package auth

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	mrand "math/rand"
)

// 滑块拼图的几何与出图。尺寸一律以“图片像素”为准：前端把落点换算回同一口径后才
// 与 sliderTolerance 比较，两侧不能各用一套 CSS 像素。

const (
	sliderBgW   = 280 // 背景图宽
	sliderBgH   = 120 // 背景图高
	sliderPiece = 56  // 拼图块方身边长
	sliderTab   = 12  // 凸出半圆的半径（右侧与上侧各一个）
)

// 孔位取值范围：横向留出真实拖动距离，纵向保证整块（含上凸）都在图内。
const (
	sliderXMin = sliderPiece + 20
	sliderXMax = sliderBgW - sliderPiece - sliderTab - 4
	sliderYMin = sliderTab + 4
	sliderYMax = sliderBgH - sliderPiece
)

// sliderPieceW/sliderPieceH 是滑块图的包围盒尺寸：方身 + 右凸、方身 + 上凸。
const (
	sliderPieceW = sliderPiece + sliderTab
	sliderPieceH = sliderPiece + sliderTab
)

// inSliderPiece 判断拼图块局部坐标 (dx,dy) 是否落在形状内。原点取方身左上角，
// 因此 dy 可以为负（上凸部分），dx 可以到 sliderPieceW（右凸部分）。
func inSliderPiece(dx, dy int) bool {
	if dx >= 0 && dx < sliderPiece && dy >= 0 && dy < sliderPiece {
		return true
	}
	// 右凸：圆心在方身右边中点。
	if rx := dx - sliderPiece; rx*rx+sq(dy-sliderPiece/2) <= sq(sliderTab) {
		return true
	}
	// 上凸：圆心在方身上边中点。
	return sq(dx-sliderPiece/2)+dy*dy <= sq(sliderTab)
}

func sq(v int) int { return v * v }

// renderSlider 出一张滑块挑战，返回背景 PNG、滑块 PNG、孔位 X 与滑块图顶边 Y。
// 孔位 X 只回给服务端调用方；Y 是前端摆放滑块图所需的偏移，不泄露答案。
//
// seed 只定种底图（调用方传的是前端可见的 key），所以孔位必须另取 crypto/rand：
// 若从 seed 派生，拿到 key 就能按同一条随机序列算出答案，滑块形同不存在。
func renderSlider(seed []byte) (bgPNG, piecePNG []byte, secretX, pieceTop int, err error) {
	// 局部源而非全局 rand：并发出图互不干扰。
	r := mrand.New(mrand.NewSource(int64(binary.BigEndian.Uint64(padSeed(seed)))))

	x, y, err := sliderHolePos()
	if err != nil {
		return nil, nil, 0, 0, err
	}

	bg := paintSliderBackdrop(r)
	// 先留一份“挖孔前”的底图给滑块裁剪：NRGBA 没有 Clone，按行复制同一份像素。
	src := image.NewNRGBA(bg.Rect)
	copy(src.Pix, bg.Pix)

	piece := cutSliderPiece(src, x, y)
	punchSliderHole(bg, x, y)

	bgPNG, err = encodePNG(bg)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	piecePNG, err = encodePNG(piece)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	return bgPNG, piecePNG, x, y - sliderTab, nil
}

// sliderHolePos 用 crypto/rand 取孔位：范围与渲染共用同一组常量，越界即失败而不是取模折叠，
// 免得把答案分布压歪。
func sliderHolePos() (x, y int, err error) {
	raw := make([]byte, 2)
	if _, err = crand.Read(raw); err != nil {
		return 0, 0, err
	}
	x = sliderXMin + int(raw[0])*(sliderXMax-sliderXMin+1)/256
	y = sliderYMin + int(raw[1])*(sliderYMax-sliderYMin+1)/256
	return x, y, nil
}

func padSeed(seed []byte) []byte {
	if len(seed) >= 8 {
		return seed
	}
	padded := make([]byte, 8)
	copy(padded, seed)
	return padded
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paintSliderBackdrop 画一张带渐变底与随机几何物的背景。没有真实照片可嵌（离线、
// 零依赖），几何物 + 网格 + 干扰线 + 噪点同样能让“孔位”不被一行像素扫描直接定位。
//
// 网格和提亮的几何物是给“可辨认”用的：滑块图裁的是这张底，纯渐变下裁片与四周几乎同色，
// 页面上就是两团糊在一起的色块——用户无从判断有没有拼上，只能来回拖。有了格线与硬边色块，
// 拼到位时边缘会重新接上同一条线。
func paintSliderBackdrop(r *mrand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sliderBgW, sliderBgH))
	h1 := hue(r)
	h2 := hue(r)
	for y := 0; y < sliderBgH; y++ {
		for x := 0; x < sliderBgW; x++ {
			t := float64(x+y) / float64(sliderBgW+sliderBgH)
			img.SetNRGBA(x, y, blend(h1, h2, t))
		}
	}
	// 几何物：半透明圆、矩形与折线叠在渐变上。
	for i := 0; i < 14; i++ {
		c := blend(hue(r), hue(r), r.Float64())
		c.A = uint8(90 + r.Intn(120))
		if i%3 == 0 {
			fillCircle(img, r.Intn(sliderBgW), r.Intn(sliderBgH), 10+r.Intn(28), c)
		} else {
			fillRect(img, r.Intn(sliderBgW)-20, r.Intn(sliderBgH)-20, 24+r.Intn(48), 10+r.Intn(30), c)
		}
	}
	// 细网格：整张图都有硬边，裁片落回孔位时格线接得上。
	step := 14 + r.Intn(8)
	grid := color.NRGBA{18, 24, 34, uint8(26 + r.Intn(18))}
	for y := 0; y < sliderBgH; y++ {
		for x := 0; x < sliderBgW; x++ {
			if x%step != 0 && y%step != 0 {
				continue
			}
			over(img, x, y, grid)
		}
	}
	for i := 0; i < 5; i++ {
		c := blend(hue(r), hue(r), r.Float64())
		c.A = uint8(70 + r.Intn(90))
		strokeLine(img, r.Intn(sliderBgW), r.Intn(sliderBgH), r.Intn(sliderBgW), r.Intn(sliderBgH), c)
	}
	for i := 0; i < 1400; i++ { // 细噪点，压住“整块纯色”的可猜测性
		x, y := r.Intn(sliderBgW), r.Intn(sliderBgH)
		p := img.PixOffset(x, y)
		n := uint8(r.Intn(28))
		img.Pix[p] = clamp8(int(img.Pix[p]) + int(n))
		img.Pix[p+1] = clamp8(int(img.Pix[p+1]) + int(n))
		img.Pix[p+2] = clamp8(int(img.Pix[p+2]) + int(n))
	}
	return img
}

func hue(r *mrand.Rand) color.NRGBA {
	return hsvToNRGBA(r.Float64(), 0.35+r.Float64()*0.35, 0.55+r.Float64()*0.35)
}

func hsvToNRGBA(h, s, v float64) color.NRGBA {
	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	var rr, gg, bb float64
	switch i % 6 {
	case 0:
		rr, gg, bb = v, t, p
	case 1:
		rr, gg, bb = q, v, p
	case 2:
		rr, gg, bb = p, v, t
	case 3:
		rr, gg, bb = p, q, v
	case 4:
		rr, gg, bb = t, p, v
	default:
		rr, gg, bb = v, p, q
	}
	return color.NRGBA{uint8(rr * 255), uint8(gg * 255), uint8(bb * 255), 255}
}

func blend(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R) + float64(int(b.R)-int(a.R))*t),
		G: uint8(float64(a.G) + float64(int(b.G)-int(a.G))*t),
		B: uint8(float64(a.B) + float64(int(b.B)-int(a.B))*t),
		A: 255,
	}
}

func clamp8(v int) uint8 {
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func fillCircle(img *image.NRGBA, cx, cy, rad int, c color.NRGBA) {
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			if x < 0 || y < 0 || x >= sliderBgW || y >= sliderBgH {
				continue
			}
			if sq(x-cx)+sq(y-cy) <= rad*rad {
				over(img, x, y, c)
			}
		}
	}
}

func fillRect(img *image.NRGBA, x0, y0, w, h int, c color.NRGBA) {
	dst := img.SubImage(image.Rect(x0, y0, x0+w, y0+h)).(*image.NRGBA).Bounds()
	draw.Draw(img, dst, image.NewUniform(c), image.Point{}, draw.Over)
}

func strokeLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx + dy
	for step := 0; step < 4000; step++ {
		if x0 >= 0 && y0 >= 0 && x0 < sliderBgW && y0 < sliderBgH {
			over(img, x0, y0, c)
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

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func over(img *image.NRGBA, x, y int, c color.NRGBA) {
	dst := img.PixOffset(x, y)
	if c.A == 255 {
		img.Pix[dst], img.Pix[dst+1], img.Pix[dst+2], img.Pix[dst+3] = c.R, c.G, c.B, c.A
		return
	}
	a := int(c.A)
	ia := 255 - a
	img.Pix[dst] = uint8((int(c.R)*a + int(img.Pix[dst])*ia) / 255)
	img.Pix[dst+1] = uint8((int(c.G)*a + int(img.Pix[dst+1])*ia) / 255)
	img.Pix[dst+2] = uint8((int(c.B)*a + int(img.Pix[dst+2])*ia) / 255)
	img.Pix[dst+3] = clamp8(int(img.Pix[dst+3]) + a)
}

// cutSliderPiece 从“挖孔之前”的背景裁出滑块图：形状内不透明、形状外全透明，
// 边缘压一圈浅色描边，让滑块与背景在同一张图上仍能分辨。
func cutSliderPiece(src *image.NRGBA, x, y int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, sliderPieceW, sliderPieceH))
	edge := color.NRGBA{250, 250, 252, 210}
	for py := 0; py < sliderPieceH; py++ {
		for px := 0; px < sliderPieceW; px++ {
			// 局部坐标：包围盒 (0, sliderTab) 才是方身左上角。
			dx, dy := px, py-sliderTab
			if !inSliderPiece(dx, dy) {
				continue
			}
			sp := src.PixOffset(x+dx, y+dy)
			dp := out.PixOffset(px, py)
			out.Pix[dp], out.Pix[dp+1], out.Pix[dp+2] = src.Pix[sp], src.Pix[sp+1], src.Pix[sp+2]
			out.Pix[dp+3] = 255
			if onSliderEdge(dx, dy) {
				op := out.PixOffset(px, py)
				out.Pix[op], out.Pix[op+1], out.Pix[op+2] = edge.R, edge.G, edge.B
			}
		}
	}
	return out
}

// onSliderEdge 报告形状边界（四邻至少一个在形外）。
func onSliderEdge(dx, dy int) bool {
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if !inSliderPiece(dx+d[0], dy+d[1]) {
			return true
		}
	}
	return false
}

// punchSliderHole 在背景上按同一形状挖孔：压暗填充 + 浅色描边。
func punchSliderHole(bg *image.NRGBA, x, y int) {
	shadow := color.NRGBA{14, 18, 26, 185}
	edge := color.NRGBA{245, 248, 252, 200}
	for dy := -sliderTab; dy < sliderPiece; dy++ {
		for dx := 0; dx < sliderPieceW; dx++ {
			if !inSliderPiece(dx, dy) {
				continue
			}
			px, py := x+dx, y+dy
			if px < 0 || py < 0 || px >= sliderBgW || py >= sliderBgH {
				continue
			}
			if onSliderEdge(dx, dy) {
				over(bg, px, py, edge)
				continue
			}
			over(bg, px, py, shadow)
		}
	}
}
