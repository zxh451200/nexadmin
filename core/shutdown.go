package core

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"nexadmin/core/ctl"
	"nexadmin/core/setup"
)

// registerShutdown 注册本机关停接口（供 `nexadmin-server stop` 使用，不依赖信号，Windows 与 Linux 一致）。
// 只有后台启动（start 通过环境变量交来令牌）的进程才注册；前台运行时这个路径是普通 404。
// 保护：只接受回环地址的对端（看 RemoteAddr，不信转发头）+ 常量时间比较令牌；非回环一律 404，不暴露存在。
func (a *App) registerShutdown(e *gin.Engine) {
	if a.shutdownToken == "" {
		return
	}
	e.POST(ctl.ShutdownPath, func(c *gin.Context) {
		if !ctl.IsLoopback(c.Request.RemoteAddr) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if !ctl.TokenOK(a.shutdownToken, c.GetHeader(ctl.ShutdownHeader)) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": gin.H{"stopping": true}})
		go a.requestStop() // 先把响应写完，再让 Run 优雅退出
	})
}

func (a *App) requestStop() { a.stopOnce.Do(func() { close(a.stopc) }) }

// Init 是 `nexadmin-server init --config <文件>`：非交互初始化（容器 / 自动化）。
// 执行逻辑就是向导用的 setup.Runner，这里只负责装配模块钩子并拒绝重复初始化。
func (a *App) Init(ctx context.Context, file string, out io.Writer) error {
	log, err := newLogger(a.cfg)
	if err != nil {
		return err
	}
	defer log.Sync() //nolint:errcheck
	a.log = log
	mods, err := a.activeModules()
	if err != nil {
		return err
	}
	a.collect(mods)
	if setup.IsInitialized(a.cfg, a.hooks.AdminChecker, log) {
		return errors.New("系统已初始化（或无法确认未初始化），拒绝重复初始化；如需恢复出厂请用 reinit")
	}
	return setup.NewRunner(a.cfg, a.hooks, log, nil).InitFromFile(ctx, file, out)
}
