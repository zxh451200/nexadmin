// gen_icons 是一次性图标生成器（仅框架仓库使用，不下发：nexadmin new / release pack 都排除本目录）。
// 只用标准库 image/png，把 web/public/icon.svg 与 icon-maskable.svg 的图形光栅化成 PWA 需要的位图：
//
//	icon-192.png / icon-512.png   圆角方块 + 品牌渐变 + 白色 N（与 icon.svg 一致，圆角外透明）
//	icon-maskable-512.png         满版渐变，N 缩在安全区内（与 icon-maskable.svg 一致；系统会自行裁切）
//	apple-touch-icon.png (180)    满版不透明（iOS 自己加圆角，透明会变黑底），图形同 maskable
//
// 渐变两端取自 web/src/framework/theme/tokens.css 的 --brand-from(243 85% 58%) / --brand-to(192 90% 42%)，
// 即 SVG 里的 #3942ef -> #0ba5cb，沿对角线（左上 -> 右下）线性插值。
//
// 用法（在项目根）：go run ./scripts/gen_icons [-out web/public]
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

type pt struct{ x, y float64 }

// 坐标系与 SVG 相同（viewBox 0 0 512 512）。
var (
	// icon.svg: M152 384V128h44l120 160V128h44v256h-44L196 224v160z
	glyphAny = []pt{{152, 384}, {152, 128}, {196, 128}, {316, 288}, {316, 128}, {360, 128}, {360, 384}, {316, 384}, {196, 224}, {196, 384}}
	// icon-maskable.svg: M176 352V160h33l90 120V160h33v192h-33l-90-120v120z
	glyphMask = []pt{{176, 352}, {176, 160}, {209, 160}, {299, 280}, {299, 160}, {332, 160}, {332, 352}, {299, 352}, {209, 232}, {209, 352}}
)

const (
	cornerR = 112.0 // icon.svg 的 rx
	ss      = 4     // 每个方向的超采样数（抗锯齿）
)

var (
	from = [3]float64{0x39, 0x42, 0xef}
	to   = [3]float64{0x0b, 0xa5, 0xcb}
)

func inPoly(p []pt, x, y float64) bool { // 奇偶规则射线法
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		if (p[i].y > y) != (p[j].y > y) && x < (p[j].x-p[i].x)*(y-p[i].y)/(p[j].y-p[i].y)+p[i].x {
			in = !in
		}
	}
	return in
}

func inRoundRect(x, y float64) bool {
	if x < 0 || y < 0 || x > 512 || y > 512 {
		return false
	}
	cx, cy := x, y
	switch {
	case x < cornerR:
		cx = cornerR
	case x > 512-cornerR:
		cx = 512 - cornerR
	}
	switch {
	case y < cornerR:
		cy = cornerR
	case y > 512-cornerR:
		cy = 512 - cornerR
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= cornerR*cornerR
}

// render 画一张 size×size 的图。rounded=true 圆角外透明；glyph 为白色图形。
func render(size int, rounded bool, glyph []pt) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := 512 / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var bg, fg int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * scale
					y := (float64(py) + (float64(sy)+0.5)/ss) * scale
					if rounded && !inRoundRect(x, y) {
						continue
					}
					bg++
					if inPoly(glyph, x, y) {
						fg++
					}
				}
			}
			if bg == 0 {
				continue
			}
			// 渐变：objectBoundingBox 的 (0,0)->(1,1)，方形上 t = (x+y)/1024，在像素中心取值。
			t := (float64(px) + 0.5 + float64(py) + 0.5) / (2 * float64(size))
			w := float64(fg) / float64(bg) // 白色占比
			var c [3]float64
			for i := range c {
				g := from[i] + (to[i]-from[i])*t
				c[i] = g + (255-g)*w
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8(c[0] + 0.5), uint8(c[1] + 0.5), uint8(c[2] + 0.5), uint8(255*float64(bg)/(ss*ss) + 0.5)})
		}
	}
	return img
}

func write(path string, img image.Image) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	return f.Close()
}

func main() {
	out := flag.String("out", filepath.Join("web", "public"), "输出目录")
	flag.Parse()
	files := []struct {
		name    string
		size    int
		rounded bool
		glyph   []pt
	}{
		{"icon-192.png", 192, true, glyphAny},
		{"icon-512.png", 512, true, glyphAny},
		{"icon-maskable-512.png", 512, false, glyphMask},
		{"apple-touch-icon.png", 180, false, glyphMask},
	}
	for _, f := range files {
		p := filepath.Join(*out, f.name)
		if err := write(p, render(f.size, f.rounded, f.glyph)); err != nil {
			fmt.Fprintln(os.Stderr, "gen_icons:", err)
			os.Exit(1)
		}
		fmt.Println("wrote", p)
	}
}
