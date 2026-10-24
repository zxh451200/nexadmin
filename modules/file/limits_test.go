package file

import (
	"context"
	"testing"

	"nexadmin/core"
)

func TestMergeLimitsFallsBackToConfigYAML(t *testing.T) {
	base := Limits{MaxImageMB: 10, MaxVideoMB: 100, MaxFileMB: 0}
	// 设置页全空（0 / 空列表）：全部回落 config.yaml。
	if got := mergeLimits(base, core.UploadLimits{}); got.MaxImageMB != 10 || got.MaxVideoMB != 100 || got.MaxFileMB != 0 || len(got.AllowedTypes) != 0 {
		t.Fatalf("empty settings must keep config.yaml values, got %+v", got)
	}
	// 设置页设了的项优先，没设的项仍回落。
	got := mergeLimits(base, core.UploadLimits{MaxImageMB: 2.5, MaxFileMB: 30, AllowedTypes: []string{"image/png", ".pdf"}})
	if got.MaxImageMB != 2.5 || got.MaxVideoMB != 100 || got.MaxFileMB != 30 || len(got.AllowedTypes) != 2 {
		t.Fatalf("merged = %+v", got)
	}
	if len(base.AllowedTypes) != 0 {
		t.Fatal("base must not be mutated")
	}
}

func TestSettingsLimitsProviderWiring(t *testing.T) {
	base := Limits{MaxImageMB: 10}
	f := settingsLimits(base)
	ctx := context.Background()

	core.SetUploadLimitsSource(nil)
	if got := f(ctx); got.MaxImageMB != 10 {
		t.Fatalf("no source registered must use config.yaml, got %+v", got)
	}

	cur := core.UploadLimits{MaxImageMB: 1, AllowedTypes: []string{"image/png"}}
	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) { return cur, nil })
	t.Cleanup(func() { core.SetUploadLimitsSource(nil) })
	if got := f(ctx); got.MaxImageMB != 1 || len(got.AllowedTypes) != 1 {
		t.Fatalf("settings must apply, got %+v", got)
	}
	cur = core.UploadLimits{MaxImageMB: 4} // 设置页再次保存后，下一次上传就用新值（没有额外缓存）
	if got := f(ctx); got.MaxImageMB != 4 || len(got.AllowedTypes) != 0 {
		t.Fatalf("updated settings must apply, got %+v", got)
	}

	core.SetUploadLimitsSource(func(context.Context) (core.UploadLimits, error) { return core.UploadLimits{}, context.DeadlineExceeded })
	if got := f(ctx); got.MaxImageMB != 10 {
		t.Fatalf("source error must fall back to config.yaml, got %+v", got)
	}
}

// 设置页的类型白名单真正拒绝不在列表里的类型，大小上限允许小数 MB。
func TestMergedLimitsEnforceTypeAndFractionalSize(t *testing.T) {
	l := mergeLimits(Limits{}, core.UploadLimits{MaxFileMB: 0.5, AllowedTypes: []string{"image/png", ".pdf"}})
	if typeAllowed(l.AllowedTypes, "application/zip", "a.zip") {
		t.Fatal("zip must be rejected by the whitelist")
	}
	if !typeAllowed(l.AllowedTypes, "image/png", "a.png") || !typeAllowed(l.AllowedTypes, "", "doc.PDF") {
		t.Fatal("whitelisted types must pass")
	}
	if mb := limitFor(l, "file"); mb != 0.5 {
		t.Fatalf("limitFor = %v", mb)
	}
}
