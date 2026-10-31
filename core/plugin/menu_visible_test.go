package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 侧边栏菜单跟随挂载状态：已注册但未挂载（未授权 / 停用）的插件菜单不显示；
// 未注册的 id 沿用现状（库里已有的菜单照旧展示）。
func TestMenuVisibleFollowsMountState(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	for id, edition := range map[string]string{"free1": "free", "pro1": "pro", "blocked": "free"} {
		dir := filepath.Join(root, "plugins", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		y := "id: " + id + "\nname: " + id + "\nversion: 1.0.0\nedition: " + edition + "\n"
		if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newManager([]Plugin{
		{ID: "free1", Edition: Free, Module: fakeModule{}},
		{ID: "pro1", Edition: Pro, Module: fakeModule{}},
		{ID: "blocked", Edition: Free, Module: fakeModule{}},
	}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }, DisabledPlugins: []string{"blocked"}})
	t.Cleanup(m.stop)

	// 模拟 Register 之后的状态（启动时挂载过的插件 live=true）。
	m.mu.Lock()
	m.entries["free1"].live = true
	m.mu.Unlock()

	cases := []struct {
		id   string
		want bool
		why  string
	}{
		{"free1", true, "free 且已挂载"},
		{"pro1", false, "pro 无授权文件，未挂载"},
		{"blocked", false, "disabled_plugins 命中"},
		{"not-registered", true, "不在二进制里：沿用现状"},
		{"../evil", true, "非法 id 不参与判断"},
	}
	for _, c := range cases {
		if got := m.MenuVisible(c.id); got != c.want {
			t.Errorf("MenuVisible(%q)=%v, want %v（%s）", c.id, got, c.want, c.why)
		}
	}
}
