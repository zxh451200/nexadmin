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
	"math"
)

// 滑块拼图的几何与出图。尺寸一律以“图片像素”为准：前端把落点换算回同一口径后才
// 与 sliderTolerance 比较，两侧不能各用一套 CSS 像素。

const (
	// sliderBgW 取登录表单的内容宽（max-w-sm = 384，输入框通栏同宽）：滑块与用户名/密码
	// 输入框对齐，对齐竞品内嵌面板的排版；手机窄屏由前端按容器宽缩放显示并换算落点。
	sliderBgW   = 384 // 背景图宽
	sliderBgH   = 150 // 背景图高（384x110 曾偏扁，宽高比 3.5:1；150 使比例落在竞品 2~2.6:1 区间）
	sliderPiece = 52  // 拼图块方身边长
	sliderTab   = 11  // 凸出半圆的半径（右侧与上侧各一个）
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

// 背景上的缺口个数（go-captcha 的 genGraphNumber）：1 = 只有真洞；≥2 = 真洞 + 干扰洞
// （假洞），拼图随机取其一——「按暗块找洞」的脚本出现歧义，须拿拼图像素逐个比对。
// 假洞由库按均分段落位，与真洞天然不重叠；改这个值不需要动校验逻辑（校验只认真洞 X）。
const sliderGraphNumber = 2

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
	// 干扰洞：与照片版同构（同排、异段，见 sliderGraphNumber），让回退路径同样有
	// 「两洞歧义」。假洞不参与校验（校验只认真洞 X），位置用装饰随机源 r 取即可；
	// 与真洞至少隔一个块宽，避免视觉上连成一片。
	for i := 0; i < 16; i++ {
		dx := sliderXMin + r.Intn(sliderXMax-sliderXMin+1)
		d := dx - x
		if d < 0 {
			d = -d
		}
		if d < sliderPieceW+8 {
			continue
		}
		punchSliderHole(bg, dx, y)
		break
	}

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

// paintSliderBackdrop 画一张「山景」底图：天空渐变 + 几朵云 + 两层山丘。
// 传统滑块验证码（极验/腾讯）都用照片级背景，裁出来的滑块才像一块「浮起的贴片」——
// 纯色/纯渐变底上裁片与四周几乎同色，看上去只剩一圈描边，用户看到的就是
// 「左边一个空框、中间一团色块」，与传统的「暗色挖孔 + 实心图块」正好颠倒。
// 山丘轮廓与云的边缘提供了强空间纹理：拼图落位时边缘重新接上同一条等高线，肉眼可辨；
// 底图仍逐张随机（云位/相位/丘高都来自 r），装饰不构成对孔位的定位线索。
func paintSliderBackdrop(r *mrand.Rand) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, sliderBgW, sliderBgH))
	// 天空：竖直渐变，顶部更亮（登录页浅色表单上视觉重量仍然偏轻）。
	skyTop := color.NRGBA{222, 227, 248, 255}
	skyBottom := color.NRGBA{183, 191, 232, 255}
	for y := 0; y < sliderBgH; y++ {
		row := blend(skyTop, skyBottom, float64(y)/float64(sliderBgH-1))
		for x := 0; x < sliderBgW; x++ {
			img.SetNRGBA(x, y, row)
		}
	}
	// 大尺度软色块：全图散布 10~14 个低透明度的明暗团（亮度在天空两端间偏移），
	// 让任何 52x52 的裁片内部都有可见的明暗结构——否则落在平缓天空区的裁片
	// 与四周几乎同色，浮起的贴片看上去仍是一个空框。
	for i := 0; i < 10+r.Intn(5); i++ {
		c := blend(skyTop, skyBottom, r.Float64())
		lift := r.Intn(36) - 18 // ±18 亮度：柔和不抢戏，但裁片边缘肉眼可辨
		c = color.NRGBA{clamp8(int(c.R) + lift), clamp8(int(c.G) + lift), clamp8(int(c.B) + lift), uint8(26 + r.Intn(26))}
		fillCircle(img, r.Intn(sliderBgW), r.Intn(sliderBgH), 22+r.Intn(34), c)
	}
	// 云：上半天 2~4 团，每团 3 个交叠的浅白圆——椭圆簇比正圆更像云。
	for i := 0; i < 2+r.Intn(3); i++ {
		cx, cy := r.Intn(sliderBgW), r.Intn(sliderBgH/2)
		c := color.NRGBA{248, 250, 255, uint8(80 + r.Intn(60))}
		for j := 0; j < 3; j++ {
			fillCircle(img, cx+j*13-13, cy+r.Intn(5), 9+r.Intn(8), c)
		}
	}
	// 山丘：两层波浪带，前层更深。正弦相位与频率逐张随机，轮廓给「落位对齐」
	// 提供跨过整个孔位区域的连续硬边——比网格线更自然，对齐与否一眼可辨。
	hills := []struct {
		base, amp int
		ph        float64
		c         color.NRGBA
	}{
		{sliderBgH - 28, 13, r.Float64() * 6.28, color.NRGBA{164, 174, 220, 255}},
		{sliderBgH - 14, 9, r.Float64() * 6.28, color.NRGBA{138, 149, 205, 255}},
	}
	for _, hl := range hills {
		freq := 0.018 + r.Float64()*0.02
		for x := 0; x < sliderBgW; x++ {
			top := int(float64(hl.base) + float64(hl.amp)*math.Sin(hl.ph+float64(x)*freq))
			for y := max(0, top); y < sliderBgH; y++ {
				img.SetNRGBA(x, y, hl.c)
			}
		}
	}
	for i := 0; i < 700; i++ { // 细噪点，压住「整块纯色」的可猜测性
		x, y := r.Intn(sliderBgW), r.Intn(sliderBgH)
		p := img.PixOffset(x, y)
		n := uint8(r.Intn(16))
		img.Pix[p] = clamp8(int(img.Pix[p]) - int(n))
		img.Pix[p+1] = clamp8(int(img.Pix[p+1]) - int(n))
		img.Pix[p+2] = clamp8(int(img.Pix[p+2]) - int(n))
	}
	return img
}

// shade 在品牌渐变的两端之间取一个同色系的装饰色：亮度随机、色相族不变。
func shade(deep, light color.NRGBA, r *mrand.Rand) color.NRGBA {
	return blend(deep, light, 0.25+r.Float64()*0.6)
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

// cutSliderPiece 从“挖孔之前”的背景裁出滑块图：形状内不透明（真裁片），边缘压一圈
// 浅色描边，形状外只放烘焙的投影（右下两档淡出）——白边 + 投影是传统滑块图块的画法，
// 让裁片在任何底色上都读成「一块浮起的贴片」，而不是一个空框。
func cutSliderPiece(src *image.NRGBA, x, y int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, sliderPieceW, sliderPieceH))
	edge := color.NRGBA{248, 250, 255, 235}
	// shadowA[k] 是「沿左上方向距形状边界 k 像素」处的投影浓度：越贴近越浓，两档淡出。
	shadowRGB := [3]uint8{56, 64, 118}
	shadowA := [3]uint8{0, 96, 160}
	for py := 0; py < sliderPieceH; py++ {
		for px := 0; px < sliderPieceW; px++ {
			// 局部坐标：包围盒 (0, sliderTab) 才是方身左上角。
			dx, dy := px, py-sliderTab
			if !inSliderPiece(dx, dy) {
				// 投影 = 形状向右下平移后露出的部分：本点在形外、其左上 k 像处在形内。
				for k := 1; k <= 2; k++ {
					if inSliderPiece(dx-k, dy-k) {
						p := out.PixOffset(px, py)
						out.Pix[p], out.Pix[p+1], out.Pix[p+2] = shadowRGB[0], shadowRGB[1], shadowRGB[2]
						out.Pix[p+3] = shadowA[k]
						break
					}
				}
				continue
			}
			sp := src.PixOffset(x+dx, y+dy)
			dp := out.PixOffset(px, py)
			out.Pix[dp], out.Pix[dp+1], out.Pix[dp+2] = src.Pix[sp], src.Pix[sp+1], src.Pix[sp+2]
			out.Pix[dp+3] = 255
			// 白描边画到 2px：单像素描边在浅底上存在感弱，2px 才有「贴片白边」的厚度。
			// 注意：真裁片测试只豁免 1px 边带，加宽前先同步它的豁免口径。
			if onSliderEdge(dx, dy) || nearSliderEdge(dx, dy) {
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

// nearSliderEdge 报告「距形状边界 2 像素以内」的内侧：白描边画成 2px 厚用的。
func nearSliderEdge(dx, dy int) bool {
	for _, d := range [8][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
		if !inSliderPiece(dx+d[0], dy+d[1]) {
			return true
		}
	}
	return false
}

// punchSliderHole 在背景上按同一形状挖孔：半透明暗填充 + 深色描边——
// 传统形态里「目标」读作一个凹陷的暗色挖孔，与浮起的浅色滑块图块形成明暗对比。
func punchSliderHole(bg *image.NRGBA, x, y int) {
	shadow := color.NRGBA{63, 71, 126, 145}
	edge := color.NRGBA{56, 64, 118, 220}
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
