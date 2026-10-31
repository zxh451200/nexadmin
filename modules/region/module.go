// Package region 提供省市区接口，数据在 core/region（go:embed，无迁移）。
// 管理员可以从数据源在线同步一份新快照，快照存在 sys_region_snapshot 并立即生效。
package region

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/region"
	"nexadmin/core/response"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "region" }

// Register 注册省市区接口：
//
//	GET  /api/v1/regions/tree?level=3   一次取回前 level 级的树（登录即可）
//	GET  /api/v1/regions?parent=<code>  按需取下级；parent 为空返回省级（登录即可）
//	GET  /api/v1/regions/status         当前数据版本与默认数据源（region:sync）
//	POST /api/v1/regions/sync           拉取数据源并生效（region:sync）
//	POST /api/v1/regions/reset          恢复随包发布的数据（region:sync）
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
	h := &Handler{svc: svc, bus: d.Bus}
	if d.Bus != nil {
		// 其它实例同步完成后重装快照；重连后做一次全量校正。
		d.Bus.On(busKind, func(_ json.RawMessage) { h.reload() }, h.reload)
	}
	r.GET("/regions/status", core.Perm("region:sync"), h.Status)
	r.POST("/regions/sync", core.Perm("region:sync"), h.Sync)
	r.POST("/regions/reset", core.Perm("region:sync"), h.Reset)
}
