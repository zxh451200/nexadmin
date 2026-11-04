package region

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/cluster"
	"nexadmin/core/response"
)

// Handler 是省市区数据同步的 HTTP 层。
type Handler struct {
	svc *Service
	bus *cluster.Bus
}

type syncRequest struct {
	Source string `json:"source"`
}

// Status GET /regions/status
func (h *Handler) Status(c *gin.Context) {
	response.OK(c, h.svc.Status())
}

// Sync POST /regions/sync：后台任务化——立即返回，拉取/校验/落库在后台协程跑，
// 进度与结果走 GET /regions/sync/status 轮询（源最大 16MB，慢源不再卡住请求）。
func (h *Handler) Sync(c *gin.Context) {
	var q syncRequest
	if err := c.ShouldBindJSON(&q); err != nil && !errors.Is(err, io.EOF) {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	actor := actorOf(c)
	if err := h.svc.StartSync(c.Request.Context(), strings.TrimSpace(q.Source), actor, h.onSynced); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"started": true})
}

// SyncStatus GET /regions/sync/status：后台同步的阶段与结果。
func (h *Handler) SyncStatus(c *gin.Context) {
	response.OK(c, h.svc.SyncStatus())
}

// onSynced 是后台同步成功后的回调：广播给其它实例重装快照。
func (h *Handler) onSynced() {
	h.bus.Publish(busKind, nil)
}

// Preview POST /regions/preview：dry-run——同一条拉取/校验链路，但不落库不生效，
// 回报这次同步会新增/删除/改名多少条以及每类最多 20 条样本。
func (h *Handler) Preview(c *gin.Context) {
	var q syncRequest
	if err := c.ShouldBindJSON(&q); err != nil && !errors.Is(err, io.EOF) {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	res, err := h.svc.Preview(c.Request.Context(), strings.TrimSpace(q.Source))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// maxUploadSize 是上传数据文件的上限，与在线拉取的 maxSourceSize 一致。
const maxUploadSize = 16 << 20

// ImportFile POST /regions/import：multipart 字段 file——手动上传数据文件
// （离线 / 内网环境没有可拉取的 HTTPS URL）。?apply=1 直接生效，默认 dry-run 只预览。
func (h *Handler) ImportFile(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("region.file_required"))
		return
	}
	// 上限按请求解析：系统参数「上传限制」能把它往小调，放大顶不开内置 16MB（见 regionMaxBytes）。
	lim := regionMaxBytes(c.Request.Context(), maxUploadSize)
	if fh.Size > lim {
		response.Fail(c, response.BadRequest("region.source_too_large", lim>>20))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, lim+1))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return
	}
	if int64(len(raw)) > lim {
		response.Fail(c, response.BadRequest("region.source_too_large", lim>>20))
		return
	}
	actor := actorOf(c)
	apply := c.Query("apply") == "1"
	res, err := h.svc.ImportFile(c.Request.Context(), raw, actor, apply)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if apply {
		h.bus.Publish(busKind, nil)
	}
	response.OK(c, res)
}

// Reset POST /regions/reset：删除同步快照，退回随包发布的数据（退回前当前快照会留一个版本）。
func (h *Handler) Reset(c *gin.Context) {
	if _, err := h.svc.Reset(c.Request.Context(), actorOf(c)); err != nil {
		response.Fail(c, err)
		return
	}
	h.bus.Publish(busKind, nil)
	response.OK(c, h.svc.Status())
}

// Versions GET /regions/versions：历史版本列表（新→旧，不含整棵树正文）。
func (h *Handler) Versions(c *gin.Context) {
	list, err := h.svc.ListVersions(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Version GET /regions/versions/:version：单个版本的完整记录（含整棵树，前端只取摘要）。
func (h *Handler) Version(c *gin.Context) {
	version, ok := parseVersion(c)
	if !ok {
		return
	}
	detail, err := h.svc.GetVersion(c.Request.Context(), version)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, detail)
}

// RollbackVersion POST /regions/versions/:version/rollback：回滚到那个版本——
// 那份快照重新走一次完整校验与落库，版本继续递增，回滚本身还能再回滚。
// 生效通知复用同步的广播（busKind），其它实例装的是同一份数据。
func (h *Handler) RollbackVersion(c *gin.Context) {
	version, ok := parseVersion(c)
	if !ok {
		return
	}
	res, err := h.svc.RollbackToVersion(c.Request.Context(), version, actorOf(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.bus.Publish(busKind, nil)
	response.OK(c, res)
}

// actorOf 取当前登录用户名，作为版本历史里的操作人；取不到（如内部调用）留空。
func actorOf(c *gin.Context) string {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return cl.Username
	}
	return ""
}

// parseVersion 解析路径里的版本号：非数字或越界一律 400，不把 0 / 负数当成合法版本发进 SQL。
func parseVersion(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("version"))
	if err != nil || n < 1 {
		response.Fail(c, response.BadRequest("region.version_invalid", c.Param("version")))
		return 0, false
	}
	return n, true
}

// reload 是其它实例同步成功后的回调：重新从库里装载快照。
func (h *Handler) reload() {
	h.svc.Load(context.Background())
}
