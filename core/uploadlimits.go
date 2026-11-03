package core

import (
	"context"
	"strconv"
	"sync/atomic"
)

// UploadLimits 是设置页（系统参数「上传限制」）里保存的上传限制。大小单位 MB。
// 0 / 空列表表示“设置页没有设”，由文件模块回落到 config.yaml 的 upload.*（它再为 0 才是不限制）。
type UploadLimits struct {
	MaxImageMB, MaxVideoMB, MaxFileMB float64
	AllowedTypes                      []string
}

var uploadSource atomic.Pointer[func(ctx context.Context) (UploadLimits, error)]

// SetUploadLimitsSource 由系统参数模块登记设置页上传限制的来源（core 与文件模块都不 import 它）；传 nil 取消。
func SetUploadLimitsSource(fn func(ctx context.Context) (UploadLimits, error)) {
	if fn == nil {
		uploadSource.Store(nil)
		return
	}
	uploadSource.Store(&fn)
}

// UploadLimitsFromSettings 返回设置页的上传限制；没人登记或读取失败时 ok=false（调用方应使用 config.yaml 的值）。
func UploadLimitsFromSettings(ctx context.Context) (l UploadLimits, ok bool) {
	fn := uploadSource.Load()
	if fn == nil {
		return UploadLimits{}, false
	}
	l, err := (*fn)(ctx)
	if err != nil {
		return UploadLimits{}, false
	}
	return l, true
}

// MBText 把字节数写成给人看的上限：2MB、2.5MB（去掉小数尾巴）。
// 报错文案与界面提示都用它，免得每处自己格式化一遍。
func MBText(bytes int64) string {
	return strconv.FormatFloat(float64(bytes)/(1<<20), 'f', -1, 64) + "MB"
}

// ResolveUploadCeiling 把设置页里的 MB 上限换算成字节：<=0（未设或「不限制」）保留调用方的 def，
// 超过 ceilingBytes 时截到 ceilingBytes。各上传入口的「参数优先 + 内置默认 + 硬顶」口径统一在这里，
// 免得每个插件各写一遍换算、改一处漏一处。
func ResolveUploadCeiling(mb float64, def, ceilingBytes int64) int64 {
	if mb <= 0 {
		return def
	}
	return min(int64(mb*1024*1024), ceilingBytes)
}
