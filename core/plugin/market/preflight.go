package market

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"nexadmin/core"
	"nexadmin/core/ctl"
)

type Environment struct {
	Ready      bool     `json:"ready"`
	CanRestart bool     `json:"canRestart"`
	Issues     []string `json:"issues"`
}

func CheckEnvironment(root string) Environment {
	return checkEnvironment(root, exec.LookPath, func() bool {
		cfg, err := core.LoadConfig(filepath.Join(root, "config", "default.yaml"), filepath.Join(root, "config", "config.yaml"))
		if err != nil {
			return false
		}
		if !filepath.IsAbs(cfg.DataDir) {
			cfg.DataDir = filepath.Join(root, cfg.DataDir)
		}
		exe, err := os.Executable()
		return err == nil && ctl.New(cfg, exe, io.Discard).CheckRestart() == nil
	})
}

func checkEnvironment(root string, lookPath func(string) (string, error), restartable func() bool) Environment {
	e := Environment{Issues: []string{}}
	for _, rel := range []string{"go.mod", "cmd/server/main.go", "plugins/plugins.go", "web/package.json"} {
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || !st.Mode().IsRegular() {
			e.Issues = append(e.Issues, "source")
			break
		}
	}
	for _, tool := range []string{"go", "npm"} {
		if _, err := lookPath(tool); err != nil {
			e.Issues = append(e.Issues, tool)
		}
	}
	for _, pkg := range []string{"vite", "typescript"} {
		if st, err := os.Stat(filepath.Join(root, "web", "node_modules", pkg, "package.json")); err != nil || !st.Mode().IsRegular() {
			e.Issues = append(e.Issues, "dependencies")
			break
		}
	}
	e.CanRestart = restartable()
	if !e.CanRestart {
		e.Issues = append(e.Issues, "restart")
	}
	e.Ready = len(e.Issues) == 0
	return e
}
