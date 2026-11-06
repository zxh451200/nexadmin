// Package region 提供省市区接口，数据在 core/region（go:embed，无迁移）。
// 管理员可以从数据源在线同步一份新快照，快照存在 sys_region_snapshot 并立即生效；
// 每次覆盖前那份旧快照会进 sys_region_snapshot_version，历史可回看、可回滚。
package region

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/region"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "region" }

// Register 注册省市区接口：
//
//	GET  /api/v1/regions/tree?level=3            一次取回前 level 级的树（登录即可）
//	GET  /api/v1/regions?parent=<code>           按需取下级；parent 为空返回省级（登录即可）
//	GET  /api/v1/regions/status                  当前数据版本与默认数据源（region:sync）
//	POST /api/v1/regions/sync                    拉取数据源并生效（region:sync）
//	POST /api/v1/regions/reset                   恢复随包发布的数据（region:sync）
//	GET  /api/v1/regions/versions                历史版本列表（region:sync）
//	GET  /api/v1/regions/versions/:version       单个历史版本（region:sync）
//	POST /api/v1/regions/versions/:version/rollback  回滚到某个历史版本（region:sync）
//
// 版本历史的读与回滚都沿用 region:sync：它能做的事与「同步一次数据」完全同量级
// （都是替换全平台在用的那一份区划数据），没有一个「只能看历史、不能改数据」的中间档，
// 所以不新增权限码；新增权限码会让既有角色默认看不到历史，反而是能力回退。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	r.GET("/regions/tree", func(c *gin.Context) {
		level, _ := strconv.Atoi(c.Query("level"))
		tree, err := region.Tree(level)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, tree)
	})
	r.GET("/regions", func(c *gin.Context) {
		list, ok, err := region.Children(c.Query("parent"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !ok {
			response.Fail(c, response.NotFound("error.not_found"))
			return
		}
		response.OK(c, list)
	})

	// 没有数据库（单测手工装配）时不注册同步接口：只读接口照常可用。
	if d == nil || d.DB == nil {
		return
	}
	svc := NewService(d.DB)
	svc.log = d.Log
	if d.Config != nil {
		// 默认数据源来自 config.yaml 的 region.*，页面只做预填与临时覆盖
		svc.sourceURL = strings.TrimSpace(d.Config.Region.SourceURL)
		svc.sourceLicense = strings.TrimSpace(d.Config.Region.SourceLicense)
	}
	// 启动时把上次同步的快照装进内存；失败只记日志，继续用内置数据。
	svc.Load(context.Background())

	// 定时自动同步随优雅关闭退出（与 message/recycle 同口径：OnClose 关闭 stop 通道）。
	stop := make(chan struct{})
	if d.OnClose != nil {
		d.OnClose(func() { close(stop) })
	}
	hours := 0
	if d.Config != nil {
		hours = d.Config.Region.AutoSyncHours
	}
	var onSynced func()
	if d.Bus != nil {
		onSynced = func() { d.Bus.Publish(busKind, nil) }
	}
	go autoSyncLoop(stop, svc, d.Log, hours, onSynced)

	h := &Handler{svc: svc, bus: d.Bus}
	if d.Bus != nil {
		// 其它实例同步完成后重装快照；重连后做一次全量校正。
		d.Bus.On(busKind, func(_ json.RawMessage) { h.reload() }, h.reload)
	}
	r.GET("/regions/status", core.Perm("region:sync"), h.Status)
	r.GET("/regions/sync/status", core.Perm("region:sync"), h.SyncStatus)
	r.POST("/regions/preview", core.Perm("region:sync"), core.RequirePlatform(), h.Preview)
	r.POST("/regions/import", core.Perm("region:sync"), core.RequirePlatform(), h.ImportFile)
	r.POST("/regions/sync", core.Perm("region:sync"), core.RequirePlatform(), h.Sync)
	r.POST("/regions/reset", core.Perm("region:sync"), core.RequirePlatform(), h.Reset)
	r.GET("/regions/versions", core.Perm("region:sync"), h.Versions)
	r.GET("/regions/versions/:version", core.Perm("region:sync"), h.Version)
	r.POST("/regions/versions/:version/rollback", core.Perm("region:sync"), core.RequirePlatform(), h.RollbackVersion)
}

// autoSyncLoop 按配置间隔自动同步省市区数据（region.auto_sync_hours，<=0 表示不自动同步——
// 出厂值 0，不打外部地址；>0 等于管理员明确授权了这台机器按期访问数据源）。
// 每小时醒一次：从未同步过（在用内置数据）或距上次同步已超过间隔，才发起一次后台同步
// （actor 记为 auto，与手动同步共用同一条链路和并发闸）；发起失败只记日志，下一轮再试。
// stop 关闭（进程优雅关闭）时立即退出，不再排下一次。
func autoSyncLoop(stop <-chan struct{}, svc *Service, log *zap.Logger, hours int, onSynced func()) {
	if hours <= 0 || svc == nil {
		return
	}
	interval := time.Duration(hours) * time.Hour
	run := func() {
		last, ok, err := svc.lastSyncedAt(context.Background())
		if err != nil {
			log.Warn("region: auto sync check failed", zap.Error(err))
			return
		}
		if ok && time.Since(last) < interval {
			return
		}
		// 复用人工同步的入口：并发闸（409 conflict）、来源回退（上次的源 → 默认源）、
		// 后台执行与状态轮询全部同一条链路；自动同步不会插队，有手动同步在跑就这轮让路。
		if err := svc.StartSync(context.Background(), "", autoSyncActor, onSynced); err != nil {
			log.Warn("region: auto sync skipped", zap.Error(err))
		}
	}
	run()
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			run()
		}
	}
}
