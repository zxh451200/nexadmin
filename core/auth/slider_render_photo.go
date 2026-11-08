package auth

// 照片级滑块渲染：开源库 go-captcha v2（Apache-2.0）+ 官方素材包（实拍背景、带阴影/蒙版的拼图形状，
// 内嵌二进制）。与手绘版 renderSlider 同契约——背景 dataURI、抠块 dataURI、孔位 X 只留在
// 服务端、Y 给前端摆块；拖拽与校验协议零变化：x 容差、轨迹行为校验全部走原逻辑，
// 前端 rc-slider-captcha 与登录接口无感。素材初始化失败时上层回退手绘版，登录入口不因装饰性资源挂掉。
//
// 防预标记：素材共 21 张（v2 实拍/渲染 16 + v1 插画 5），且每次出图叠加一道随机色调扰动
// （逐通道增益 + 亮度偏移，同一运算同扣背景与抠块）——攻击者没法用固定像素指纹「认图建库」，
// 孔位搜索空间回不到「背图查表」。
//
// 图形尺寸钉死在 sliderPiece（63 = 52 方身 + 11 凸起）：Go 侧把拼图缩放到该尺寸，
// 挑战响应里的 pieceWidth/pieceHeight 常量才保持与抠块实际边界精确一致。

import (
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	mrand "math/rand"
	"sync"

	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/slide"
	assets_v1_1 "github.com/wenlng/go-captcha-assets/bindata/images/image_1"
	assets_v1_2 "github.com/wenlng/go-captcha-assets/bindata/images/image_2"
	assets_v1_3 "github.com/wenlng/go-captcha-assets/bindata/images/image_3"
	assets_v1_4 "github.com/wenlng/go-captcha-assets/bindata/images/image_4"
	assets_v1_5 "github.com/wenlng/go-captcha-assets/bindata/images/image_5"
	assets_v2_1 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_1"
	assets_v2_2 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_2"
	assets_v2_3 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_3"
	assets_v2_4 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_4"
	assets_v2_5 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_5"
	assets_v2_6 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_6"
	assets_v2_7 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_7"
	assets_v2_8 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_8"
	assets_v2_9 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_9"
	assets_v2_10 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_10"
	assets_v2_11 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_11"
	assets_v2_12 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_12"
	assets_v2_13 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_13"
	assets_v2_14 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_14"
	assets_v2_15 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_15"
	assets_v2_16 "github.com/wenlng/go-captcha-assets/bindata/images/image_v2_16"
	assets_tile_1 "github.com/wenlng/go-captcha-assets/bindata/tiles/tile_1"
	assets_tile_2 "github.com/wenlng/go-captcha-assets/bindata/tiles/tile_2"
	assets_tile_3 "github.com/wenlng/go-captcha-assets/bindata/tiles/tile_3"
	assets_tile_4 "github.com/wenlng/go-captcha-assets/bindata/tiles/tile_4"
	"github.com/wenlng/go-captcha-assets/helper"
)

var (
	photoBGOnce sync.Once
	photoBGs    []image.Image
	photoBGErr  error

	slidePhotoOnce sync.Once
	slidePhotoCap  slide.Captcha
	slidePhotoErr  error
)

// sharedPhotoBackgrounds 惰性解码背景素材池（21 张），滑块与点选两个 builder 共用一份，
// 21 张解码后的常驻内存约十几 MB，不该按形态各存一份。
func sharedPhotoBackgrounds() ([]image.Image, error) {
	photoBGOnce.Do(func() { photoBGs, photoBGErr = photoBackgrounds() })
	return photoBGs, photoBGErr
}

// slidePhotoCaptcha 惰性构建照片版滑块：素材解码只做一次，全进程复用同一个 builder。
func slidePhotoCaptcha() (slide.Captcha, error) {
	slidePhotoOnce.Do(func() {
		bgs, err := sharedPhotoBackgrounds()
		if err != nil {
			slidePhotoErr = err
			return
		}
		graphs, err := photoGraphs()
		if err != nil {
			slidePhotoErr = err
			return
		}
		b := slide.NewBuilder(
			slide.WithImageSize(option.Size{Width: sliderBgW, Height: sliderBgH}),
			// 拼图整体（方身 + 凸起都在形状画布内）钉到 sliderPieceW：挑战响应里的
			// pieceWidth/pieceHeight 常量、前端摆块尺寸与抠块实际边界三方一致。
			slide.WithRangeGraphSize(option.RangeVal{Min: sliderPieceW, Max: sliderPieceW}),
			// 单缺口：干扰洞与真洞视觉完全同构，对「按暗块定位」的脚本不构成歧义，
			// 只让人多看一眼；防御靠 crypto/rand 孔位、逐图色调扰动与轨迹校验。
			// Generate 返回的 block 即唯一真洞，X 合法区间的约束重试只作用它。
			slide.WithGenGraphNumber(1),
		)
		b.SetResources(slide.WithBackgrounds(bgs), slide.WithGraphImages(graphs))
		slidePhotoCap = b.Make()
	})
	return slidePhotoCap, slidePhotoErr
}

// photoAsset 是一个内嵌素材：bindata 包的 Asset 函数 + 资源名。
type photoAsset struct {
	asset func(string) ([]byte, error)
	name  string
}

// photoBackgrounds 解码内嵌的背景素材：v2 渲染/实拍 16 张 + v1 插画 5 张，共 21 张。
// 池子够大 + 出图随机扰动，预标记单张图才换不来稳定的孔位答案。
func photoBackgrounds() ([]image.Image, error) {
	list := []photoAsset{
		{assets_v2_1.Asset, "sourcedata/images/image-v2-1/image.jpg"},
		{assets_v2_2.Asset, "sourcedata/images/image-v2-2/image.jpg"},
		{assets_v2_3.Asset, "sourcedata/images/image-v2-3/image.jpg"},
		{assets_v2_4.Asset, "sourcedata/images/image-v2-4/image.jpg"},
		{assets_v2_5.Asset, "sourcedata/images/image-v2-5/image.jpg"},
		{assets_v2_6.Asset, "sourcedata/images/image-v2-6/image.jpg"},
		{assets_v2_7.Asset, "sourcedata/images/image-v2-7/image.jpg"},
		{assets_v2_8.Asset, "sourcedata/images/image-v2-8/image.jpg"},
		{assets_v2_9.Asset, "sourcedata/images/image-v2-9/image.jpg"},
		{assets_v2_10.Asset, "sourcedata/images/image-v2-10/image.jpg"},
		{assets_v2_11.Asset, "sourcedata/images/image-v2-11/image.jpg"},
		{assets_v2_12.Asset, "sourcedata/images/image-v2-12/image.jpg"},
		{assets_v2_13.Asset, "sourcedata/images/image-v2-13/image.jpg"},
		{assets_v2_14.Asset, "sourcedata/images/image-v2-14/image.jpg"},
		{assets_v2_15.Asset, "sourcedata/images/image-v2-15/image.jpg"},
		{assets_v2_16.Asset, "sourcedata/images/image-v2-16/image.jpg"},
		{assets_v1_1.Asset, "sourcedata/images/image-1/image.jpg"},
		{assets_v1_2.Asset, "sourcedata/images/image-2/image.jpg"},
		{assets_v1_3.Asset, "sourcedata/images/image-3/image.jpg"},
		{assets_v1_4.Asset, "sourcedata/images/image-4/image.jpg"},
		{assets_v1_5.Asset, "sourcedata/images/image-5/image.jpg"},
	}
	out := make([]image.Image, 0, len(list))
	for _, p := range list {
		raw, err := p.asset(p.name)
		if err != nil {
			return nil, err
		}
		img, err := helper.DecodeByteToJpeg(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, nil
}

// photoGraphs 解码四种拼图形状（凸起 + 阴影 + 蒙版三件套）。
func photoGraphs() ([]*slide.GraphImage, error) {
	pkgs := []struct {
		asset func(string) ([]byte, error)
		base  string
	}{
		{assets_tile_1.Asset, "sourcedata/tiles/tile-1/"},
		{assets_tile_2.Asset, "sourcedata/tiles/tile-2/"},
		{assets_tile_3.Asset, "sourcedata/tiles/tile-3/"},
		{assets_tile_4.Asset, "sourcedata/tiles/tile-4/"},
	}
	out := make([]*slide.GraphImage, 0, len(pkgs))
	for _, p := range pkgs {
		png := func(name string) (image.Image, error) {
			raw, err := p.asset(p.base + name)
			if err != nil {
				return nil, err
			}
			return helper.DecodeByteToPng(raw)
		}
		overlay, err := png("tile.png")
		if err != nil {
			return nil, err
		}
		shadow, err := png("tile-shadow.png")
		if err != nil {
			return nil, err
		}
		mask, err := png("tile-mask.png")
		if err != nil {
			return nil, err
		}
		out = append(out, &slide.GraphImage{OverlayImage: overlay, ShadowImage: shadow, MaskImage: mask})
	}
	return out, nil
}

// photoTone 是一次随机色调扰动：逐通道增益 + 亮度偏移。每次出图各抽一组，
// 同一道点运算同时作用于背景与抠块——抠块本就裁自背景同源像素，点运算保序，
// 拼回孔位时色彩严丝合缝；而每次挑战的像素指纹都不同，「按图预标记」的攻击失效。
type photoTone struct{ gR, gG, gB, oR, oG, oB float64 }

func randPhotoTone() photoTone {
	var seed [8]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return photoTone{1, 1, 1, 0, 0, 0} // 熵源挂了就放弃扰动，出图不能因此失败
	}
	r := mrand.New(mrand.NewSource(int64(binary.BigEndian.Uint64(seed[:]))))
	gain := func() float64 { return 0.88 + r.Float64()*0.24 }  // 0.88~1.12：肉眼几乎无感
	offset := func() float64 { return float64(r.Intn(25) - 12) } // ±12：同上
	return photoTone{gain(), gain(), gain(), offset(), offset(), offset()}
}

// applyTone 把色调运算画到一份 NRGBA 副本上：alpha 原样保留（抠块的投影半透明像素不破）。
func applyTone(img image.Image, t photoTone) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i] = clamp8(int(float64(out.Pix[i])*t.gR + t.oR))
		out.Pix[i+1] = clamp8(int(float64(out.Pix[i+1])*t.gG + t.oG))
		out.Pix[i+2] = clamp8(int(float64(out.Pix[i+2])*t.gB + t.oB))
	}
	return out
}

// renderSliderPhoto 生成一张照片级挑战：背景与抠块都是带前缀的 dataURI，
// 孔位 X 返回给调用方存入缓存（不出网），Y 给前端把抠块摆在正确的一行。
//
// go-captcha 自己的 X 分布允许孔位贴近左缘——离起点太近的孔会让「拖动推进量 ≥ 40px」的
// 行为判定永远过不了，属于功能性退化，所以按与手绘版相同的孔位区间（sliderXMin..sliderXMax）
// 做约束重试；连试不中返回错误，上层回退手绘版。
func renderSliderPhoto() (bg, piece string, secretX, pieceTop int, err error) {
	capt, err := slidePhotoCaptcha()
	if err != nil {
		return "", "", 0, 0, err
	}
	var perr error
	for attempt := 0; attempt < 16; attempt++ {
		data, gerr := capt.Generate()
		if gerr != nil {
			return "", "", 0, 0, gerr
		}
		block := data.GetData()
		if block.X < sliderXMin || block.X > sliderXMax {
			continue
		}
		// 背景与抠块各自重编码为 PNG dataURI（与手绘版同形状，测试按 PNG 解码校尺寸/透明度），
		// 编码前各叠同一道随机色调扰动——抠块是半透明 PNG，alpha 原样保留。
		tone := randPhotoTone()
		var buf bytes.Buffer
		if perr = png.Encode(&buf, applyTone(data.GetMasterImage().Get(), tone)); perr != nil {
			return "", "", 0, 0, perr
		}
		bg = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		buf.Reset()
		if perr = png.Encode(&buf, applyTone(data.GetTileImage().Get(), tone)); perr != nil {
			return "", "", 0, 0, perr
		}
		piece = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		return bg, piece, block.X, block.Y, nil
	}
	return "", "", 0, 0, errSliderRenderNoFit
}

// errSliderRenderNoFit 表示连试多次都摆不出合法孔位——交由上层回退手绘渲染。
var errSliderRenderNoFit = errors.New("slider: no valid notch position after retries")
