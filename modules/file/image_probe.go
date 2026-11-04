package file

import (
	"image"
	"io"
	"strings"

	"go.uber.org/zap"

	// 图片格式注册表必须写在这个包里，不能指望别处顺带导入：
	// image.DecodeConfig 只认已注册的格式，少了这一行，站内默认允许的 .jpg/.gif
	// 会静默存成 0×0（PNG 此前只是 core/auth/slider_render.go 导入 image/png 才碰巧能用）。
	// webp 同样是默认允许的类型，标准库没有解码器，用 golang.org/x/image/webp。
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// imageDimensions 读出图片的像素宽高，返回 0 表示「非图片 / 读不出」——调用方按 0 存库。
// 读之前必须把游标回到开头，读完不要求游标位置（上传完成路径随后还会 Seek(0) 算哈希）。
//
// 失败一律留一条日志：静默存 0 和「这个格式根本没注册」在数据上长得一模一样，
// 只能从日志区分。SVG 是唯一按设计没有像素尺寸的允许类型，所以不问、也不报警。
func imageDimensions(r io.Reader, name, mimeType string) (w, h int) {
	if !strings.EqualFold(mimeType, "image/svg+xml") && !hasExt(strings.ToLower(name), ".svg") {
		cfg, format, err := image.DecodeConfig(r)
		if err != nil {
			zap.L().Warn("file: 图片宽高探测失败，按 0 存储",
				zap.String("name", name), zap.String("mime", mimeType), zap.Error(err))
			return 0, 0
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			zap.L().Warn("file: 图片宽高探测到 0，按 0 存储",
				zap.String("name", name), zap.String("format", format))
			return 0, 0
		}
		return cfg.Width, cfg.Height
	}
	return 0, 0
}
