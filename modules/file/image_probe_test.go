package file

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// 图片宽高探测（image_probe.go）的契约：站内默认允许上传的图片格式都要能读出尺寸。
// 这几条用例的实际作用是格式注册表——_ "image/jpeg" 之类的空白导入一旦被删，
// DecodeConfig 会返回 "image: unknown format"，于是每个 .jpg 静默存成 0×0，
// 而这里会以「0×0, want 80×60」的形式红给你看。

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageDimensionsReadsEachFormat(t *testing.T) {
	var jpegBuf, gifBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, image.NewRGBA(image.Rect(0, 0, 80, 60)), nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 33, 17), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, mime   string
		wantW, wantH int
		data         []byte
	}{
		{"a.png", "image/png", 320, 240, encodePNG(t, 320, 240)},
		{"a.jpg", "image/jpeg", 80, 60, jpegBuf.Bytes()},
		{"a.gif", "image/gif", 33, 17, gifBuf.Bytes()},
		{"a.webp", "image/webp", 640, 480, webpLosslessHeader(t, 640, 480)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := imageDimensions(bytes.NewReader(tc.data), tc.name, tc.mime)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Errorf("imageDimensions(%s) = %d×%d, want %d×%d（该格式的解码器没注册？看 image_probe.go 的空白导入）",
					tc.name, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

// webpLosslessHeader 造一张只含 VP8L 头的 webp：宽高各占 14 bit（存的是尺寸-1），
// 探测只读这一段头，不需要完整像素数据。
func webpLosslessHeader(t *testing.T, w, h int) []byte {
	t.Helper()
	if w > 16384 || h > 16384 {
		t.Fatalf("VP8L 头每个维度只有 14 bit：%d×%d", w, h)
	}
	var bits uint32
	bits |= uint32(w-1) & 0x3FFF     // bit0-13：宽-1
	bits |= uint32(h-1&0x3FFF) << 14 // bit14-27：高-1
	// bit28 = alpha（0），bit29-31 = 版本号（必须是 0）
	body := append([]byte{0x2f}, byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24))

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(4+len("VP8L")+8+len(body)))
	buf.WriteString("WEBP")
	buf.WriteString("VP8L")
	binary.Write(&buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	return buf.Bytes()
}

// 读不出来的东西一律 0×0，不报错也不阻断上传：文件本身合法，只是没有像素尺寸可记。
func TestImageDimensionsFallsBackToZero(t *testing.T) {
	cases := []struct {
		name, mime string
		data       []byte
	}{
		{"doc.pdf", "application/pdf", []byte("%PDF-1.4 not an image at all padding padding")},
		{"broken.jpg", "image/jpeg", []byte{0xff, 0xd8, 0x00, 0x01}},
		{"empty.png", "image/png", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w, h := imageDimensions(bytes.NewReader(tc.data), tc.name, tc.mime); w != 0 || h != 0 {
				t.Errorf("非法/不支持的图片应存 0×0，得到 %d×%d", w, h)
			}
		})
	}
}

// SVG 没有像素尺寸，属按设计跳过：既不探测（否则每次上传都在日志里留一条 Warn），也不报错。
func TestImageDimensionsSkipsSVG(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)
	for _, tc := range []struct{ name, mime string }{
		{"icon.svg", "image/svg+xml"},
		{"icon.SVG", "image/png"}, // 扩展名是 svg 也按 SVG 处理（mime 可能被打码器误报）
	} {
		if w, h := imageDimensions(bytes.NewReader(svg), tc.name, tc.mime); w != 0 || h != 0 {
			t.Errorf("%s 不该被探测：%d×%d", tc.name, w, h)
		}
	}
}
