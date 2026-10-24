// Package file 是文件模块：分组、tus 断点续传上传、预览/下载、大小与类型限制。
// 表：sys_file、sys_file_group（租户隔离）。权限点：file:list / file:upload / file:delete。
package file

import (
	"mime"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core"
	"nexadmin/core/middleware"
)

func init() { core.RegisterBuiltin(module{}) }

// 权限点，与前端 file 模块的 route.tsx / 各按钮的 <Can perm> 一致。
const (
	PermList   = "file:list"   // 查看文件与分组
	PermUpload = "file:upload" // 上传、改可见性、移动、分组增改
	PermDelete = "file:delete" // 删除文件与分组
)

type module struct{}

func (module) Name() string { return "file" }

// Register 注册路由。r 要求登录；预览/下载走 d.Public（<img>、<video> 带不了令牌，靠随机串/签名保护）。
func (module) Register(r *gin.RouterGroup, d *core.Deps) {
	st, err := newStoreWith(d.Config.DataDir, d.Config.Storage)
	if err != nil {
		d.Log.Error("file module disabled: storage init failed", zap.Error(err))
		return
	}
	cfg := d.Config
	svc := NewService(d.DB, st, settingsLimits(Limits{
		MaxImageMB: float64(cfg.Upload.MaxSizeImageMB), MaxVideoMB: float64(cfg.Upload.MaxSizeVideoMB), MaxFileMB: float64(cfg.Upload.MaxSizeFileMB),
	}))
	svc.cache = d.Cache
	h := &Handler{svc: svc}
	tus := &tusHandler{svc: svc}

	r.GET("/file-groups", core.Perm(PermList), h.ListGroups)
	r.POST("/file-groups", core.Perm(PermUpload), h.CreateGroup)
	r.PUT("/file-groups/:id", core.Perm(PermUpload), h.RenameGroup)
	r.DELETE("/file-groups/:id", core.Perm(PermDelete), h.DeleteGroup)

	r.GET("/files", core.Perm(PermList), h.List)
	r.POST("/files", core.Perm(PermUpload), h.Register)
	r.PUT("/files/move", core.Perm(PermUpload), h.Move)
	r.PUT("/files/:id", core.Perm(PermUpload), h.Update)
	r.DELETE("/files/:id", core.Perm(PermDelete), h.DeleteOne)
	r.DELETE("/files", core.Perm(PermDelete), h.DeleteMany)

	// tus 断点续传：POST 创建，HEAD 查进度，PATCH 追加，DELETE 取消。上传按用户限流（规则 upload）。
	up := middleware.RateLimitRule(d.Cache, d.Log, d.Config.RateLimit, "upload", middleware.ByUser())
	r.POST("/files/upload", core.Perm(PermUpload), up, tus.Create)
	r.HEAD("/files/upload/:id", core.Perm(PermUpload), tus.Head)
	r.PATCH("/files/upload/:id", core.Perm(PermUpload), up, tus.Patch)
	r.DELETE("/files/upload/:id", core.Perm(PermUpload), tus.Terminate)

	d.Public.GET("/files/public/:token/:name", h.ServePublic)
	d.Public.GET("/files/:id/content", h.ServeSigned)
}

func dispositionAttachment(name string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": name})
}
