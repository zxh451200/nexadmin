package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameworkVersionResolution(t *testing.T) {
	root := t.TempDir()
	if v := FrameworkVersion(root, "dev"); v != "" {
		t.Errorf("无清单且 hint 不可解析应为空, got %q", v)
	}
	if v := FrameworkVersion(root, "1.4.0"); v != "1.4.0" {
		t.Errorf("hint 优先, got %q", v)
	}
	if err := os.WriteFile(filepath.Join(root, FrameworkManifestFile), []byte(`{"framework_version":"0.9.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if v := FrameworkVersion(root, "dev"); v != "0.9.1" {
		t.Errorf("hint 为 dev 时回落清单, got %q", v)
	}
}

func TestCheckRequires(t *testing.T) {
	if err := CheckRequires("", "1.0.0"); err != nil {
		t.Error("未声明应通过")
	}
	if err := CheckRequires(">=2.0.0", ""); err != nil {
		t.Error("框架版本未知应跳过校验")
	}
	if err := CheckRequires(">=1.0.0 <2.0.0", "1.5.0"); err != nil {
		t.Error(err)
	}
	if err := CheckRequires(">=2.0.0", "1.5.0"); err == nil || !strings.Contains(err.Error(), ">=2.0.0") {
		t.Errorf("应拒绝并说明范围: %v", err)
	}
	if err := CheckRequires(">=abc", "1.5.0"); err == nil {
		t.Error("无法解析的范围不能被静默放行")
	}
}

// 运行时：不满足 requires_nexadmin 的插件不挂载（状态 invalid），原因写进 Reason，即使授权有效 / 免费版。
func TestManagerRejectsUnsatisfiedRequires(t *testing.T) {
	mk := func(requires, fw string) Info {
		root := t.TempDir()
		dir := filepath.Join(root, "plugins", "cron")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		y := "id: cron\nname: cron\nversion: 1.0.0\nedition: free\n"
		if requires != "" {
			y += "requires_nexadmin: \"" + requires + "\"\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
		m := newManager([]Plugin{{ID: "cron", Edition: Free, Module: fakeModule{}}}, Options{Root: root, FrameworkVersion: fw, Hosts: []string{"x.test"}})
		return m.List()[0]
	}
	if i := mk(">=2.0.0", "1.0.0"); i.Status != StatusInvalid || !strings.Contains(i.Reason, ">=2.0.0") || i.Name != "cron" {
		t.Errorf("不满足应 invalid 并带原因: %+v", i)
	}
	if i := mk(">=1.0.0", "1.2.0"); i.Status != StatusActive {
		t.Errorf("满足应 active: %+v", i)
	}
	if i := mk(">=2.0.0", "dev"); i.Status != StatusActive {
		t.Errorf("版本未知（dev 且无清单）应跳过校验: %+v", i)
	}
}
