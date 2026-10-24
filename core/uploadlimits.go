package core

import (
	"context"
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
