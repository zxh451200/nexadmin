package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// InitFromFile 是 `nexadmin-server init --config <文件>` 的实现：非交互初始化，容器与自动化用。
// 文件内容与向导 POST /setup/execute 的请求体相同（db / cache / admin / optional），JSON 或 YAML 均可；
// 真正的执行逻辑就是向导用的 Runner.run，这里只负责读文件和打印进度（不打印密码）。
func (r *Runner) InitFromFile(ctx context.Context, path string, out io.Writer) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil { // YAML 是 JSON 的超集，一并覆盖两种格式
		return fmt.Errorf("parse %s: %w", path, err)
	}
	js, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var req executeReq
	if err := json.Unmarshal(js, &req); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var failed *stepResult
	for _, s := range r.run(ctx, &req) {
		fmt.Fprintf(out, "  %-12s %s\n", s.Key, s.Status)
		if s.Status == "failed" && failed == nil {
			s := s
			failed = &s
		}
	}
	if failed != nil {
		return fmt.Errorf("init failed at %s: %s", failed.Key, failed.Message)
	}
	return nil
}
