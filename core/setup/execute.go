package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/config"
	"nexadmin/core/database"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

type stepResult struct {
	Key     string `json:"key"`
	Status  string `json:"status"` // pending | running | done | failed
	Message string `json:"message,omitempty"`
}

// execute POST /setup/execute：写配置 -> 迁移 -> 种数据 -> 建超管 -> 写标记。
// 步骤失败时仍返回 HTTP 200，由 steps 里的 failed 告诉前端卡在哪；已写的 config.yaml 会回滚，可改参数重试。
func (r *Runner) execute(c *gin.Context) {
	if !r.mu.TryLock() {
		response.Fail(c, response.Conflict("setup.running"))
		return
	}
	defer r.mu.Unlock()

	var req executeReq
	if !response.BindJSON(c, &req) {
		return
	}
	if !strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		response.OK(c, gin.H{"steps": r.run(c.Request.Context(), &req)})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	emit := func(step stepResult) {
		c.SSEvent("step", step)
		c.Writer.Flush()
	}
	steps := r.runProgress(c.Request.Context(), &req, emit)
	c.SSEvent("complete", gin.H{"steps": steps})
	c.Writer.Flush()
}

// run 是初始化的唯一实现，向导（execute）和命令行（InitFromFile）共用。
func (r *Runner) run(parent context.Context, req *executeReq) []stepResult {
	return r.runProgress(parent, req, nil)
}

func (r *Runner) runProgress(parent context.Context, req *executeReq, emit func(stepResult)) []stepResult {
	dbCfg, verr := r.validate(req)
	keys := []string{"writeConfig", "migrate", "seed", "createAdmin", "writeMark"}
	steps := make([]stepResult, len(keys))
	for i, k := range keys {
		steps[i] = stepResult{Key: k, Status: "pending"}
	}
	update := func(i int, status string) {
		steps[i].Status = status
		if emit != nil {
			emit(steps[i])
		}
	}
	fail := func(i int, err error) []stepResult {
		msg := err.Error()
		for _, secret := range []string{r.Token, req.DB.Password, req.Cache.Password, req.Admin.Password, req.Optional.S3SecretKey, req.Optional.S3AccessKey, req.Optional.AIAPIKey} {
			msg = redact(msg, secret)
		}
		steps[i].Message = msg
		update(i, "failed")
		r.Log.Error("setup: step failed", zap.String("step", keys[i]), zap.String("error", msg))
		return steps
	}
	if verr != nil {
		return fail(0, verr)
	}

	// 初始化是不可回滚的多步写入（建库/DDL/种数据）：浏览器中途断开不能把它取消在半路。
	ctx, cancel := context.WithTimeout(tenant.Skip(context.WithoutCancel(parent)), 5*time.Minute)
	defer cancel()

	// 1. writeConfig
	update(0, "running")
	rollback, err := writeConfig(r.Cfg.UserConfigPath, r.overlay(req, dbCfg))
	if err != nil {
		return fail(0, err)
	}
	update(0, "done")
	var db *gorm.DB
	abort := func(i int, err error) []stepResult {
		if db != nil {
			_ = database.Close(db)
		}
		rollback()
		return fail(i, err)
	}

	// 2. migrate
	update(1, "running")
	newCfg, err := config.Load(r.Cfg.DefaultConfigPath, r.Cfg.UserConfigPath)
	if err != nil {
		return abort(1, err)
	}
	if req.DB.AutoCreate {
		if err := database.EnsureDatabase(ctx, newCfg.Database); err != nil {
			return abort(1, err)
		}
	}
	if db, err = database.Open(newCfg.Database); err != nil {
		return abort(1, err)
	}
	if err := idgen.Init(int64(newCfg.Database.NodeID)); err != nil {
		return abort(1, err)
	}
	sess := db.WithContext(ctx)
	if r.Hooks.Migrate == nil {
		return abort(1, errors.New("migration runner not configured"))
	}
	if err := r.Hooks.Migrate(sess); err != nil {
		return abort(1, err)
	}
	update(1, "done")

	// 3. seed
	update(2, "running")
	for _, s := range r.Hooks.Seeders {
		if err := s.Seed(sess); err != nil {
			return abort(2, err)
		}
	}
	if r.Hooks.SiteWriter != nil { // 站点名写入设置页「站点」分区（登录页 / 顶栏 / manifest 读取的位置）
		if err := r.Hooks.SiteWriter.WriteSiteName(ctx, sess, strings.TrimSpace(req.Admin.SiteName)); err != nil {
			return abort(2, err)
		}
	}
	update(2, "done")

	// 4. createAdmin
	update(3, "running")
	if r.Hooks.AdminCreator == nil {
		return abort(3, errors.New("no module provides admin creation (register the user module first)"))
	}
	admin := Admin{Username: req.Admin.Username, Password: req.Admin.Password, SiteName: req.Admin.SiteName, Language: req.Admin.Language, Timezone: req.Admin.Timezone}
	if err := r.Hooks.AdminCreator.CreateAdmin(ctx, sess, admin); err != nil {
		return abort(3, err)
	}
	update(3, "done")

	// 5. writeMark
	update(4, "running")
	_ = database.Close(db)
	marker := MarkerPath(newCfg)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
		_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		rollback()
		return fail(4, errors.New("cannot write initialization marker "+marker))
	}

	if r.OnDone != nil {
		if err := r.OnDone(); err != nil {
			r.Log.Error("setup: activate after init failed, restart required", zap.Error(err))
			steps[4].Message = "initialized, but switching to normal mode failed; please restart the server"
		}
	}
	update(4, "done")
	return steps
}
