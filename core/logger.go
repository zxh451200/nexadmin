package core

import (
	"os"

	"go.uber.org/zap"

	"nexadmin/core/config"
)

// newLogger 开发环境输出到控制台，生产环境输出 JSON 到 stdout 和 logs/nexadmin.log。
func newLogger(cfg *config.Config) (*zap.Logger, error) {
	if !cfg.IsProduction() {
		return zap.NewDevelopment()
	}
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return nil, err
	}
	zc := zap.NewProductionConfig()
	zc.OutputPaths = []string{"stdout", "logs/nexadmin.log"}
	return zc.Build()
}
