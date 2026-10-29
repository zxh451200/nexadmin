package exportcenter

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/core/storage"
	"nexadmin/pkg/idgen"
)

type handler struct {
	m   *job.Manager
	db  *gorm.DB
	log *zap.Logger
}

// jobDTO 是对外字段：不含参数、对象键和节点信息。
type jobDTO struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	Progress     int        `json:"progress"`
	Message      string     `json:"message"`
	Error        string     `json:"error"`
	OwnerID      string     `json:"ownerId"`
	OwnerName    string     `json:"ownerName"`
	FileName     string     `json:"fileName"`
	FileSize     int64      `json:"fileSize"`
	Downloadable bool       `json:"downloadable"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
}

func toDTO(j *job.Job, names map[idgen.ID]string) jobDTO {
	return jobDTO{
		ID: j.ID.String(), Type: j.Type, Status: j.Status, Progress: j.Progress, Message: j.Message, Error: j.Error,
		OwnerID: j.OwnerID.String(), OwnerName: names[j.OwnerID], FileName: j.ResultName, FileSize: j.ResultSize,
		Downloadable: j.Status == job.StatusSuccess && j.ResultFile != "",
		CreatedAt:    j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
	}
}

// ownerNames 批量取所有者账号名（用户表属于用户模块，取不到只是不显示名字）。
func (h *handler) ownerNames(c *gin.Context, rows []*job.Job) map[idgen.ID]string {
	out := map[idgen.ID]string{}
	seen := map[int64]bool{}
	var ids []int64
	for _, j := range rows {
		if !seen[int64(j.OwnerID)] {
			seen[int64(j.OwnerID)] = true
			ids = append(ids, int64(j.OwnerID))
		}
	}
	if len(ids) == 0 {
		return out
	}
	var us []struct {
		ID       idgen.ID
		Username string
	}
	if err := h.db.WithContext(c.Request.Context()).Table("sys_user").Select("id, username").Where("id IN ?", ids).Scan(&us).Error; err == nil {
		for _, u := range us {
			out[u.ID] = u.Username
		}
	}
	return out
}

// actor 取当前操作者；管理员 = 超管或拥有 job:all。取不到权限信息时按普通用户处理（失败即收紧）。
func (h *handler) actor(c *gin.Context) (job.Actor, bool) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		response.Fail(c, response.Unauthorized("error.unauthorized"))
		return job.Actor{}, false
	}
	a := job.Actor{ID: cl.UserID, Admin: cl.Super}
	if !a.Admin {
		if svc := auth.Default(); svc != nil && svc.Provider() != nil {
			if u, err := svc.Profile(c.Request.Context(), cl); err == nil {
				a.Admin = auth.HasPerm(u.Perms, PermAll)
			}
		}
	}
	return a, true
}

func (h *handler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, job.ErrNotFound):
		response.Fail(c, response.NotFound("error.not_found"))
	case errors.Is(err, job.ErrBadState):
		response.Fail(c, response.Conflict("error.conflict").WithMsg("当前状态不允许该操作"))
	case errors.Is(err, job.ErrTooMany):
		response.Fail(c, response.New(http.StatusTooManyRequests, "error.too_many"))
	default:
		response.Fail(c, err)
	}
}

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.NotFound("error.not_found"))
		return 0, false
	}
	return id, true
}

// list GET /jobs?type=&status=&page=&pageSize=&all=1（all 仅 job:all 可用）
func (h *handler) list(c *gin.Context) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	all := c.Query("all") == "1" || c.Query("all") == "true"
	if all && !a.Admin {
		response.Fail(c, response.Forbidden("error.forbidden"))
		return
	}
	page, size := response.ParsePage(c)
	rows, total, err := h.m.List(c.Request.Context(), a, job.Filter{Type: c.Query("type"), Status: c.Query("status"), Page: page, PageSize: size}, all)
	if err != nil {
		h.fail(c, err)
		return
	}
	names := h.ownerNames(c, rows)
	out := make([]jobDTO, len(rows))
	for i, j := range rows {
		out[i] = toDTO(j, names)
	}
	response.Page(c, out, total, page, size)
}

// get GET /jobs/:id
func (h *handler) get(c *gin.Context) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	j, err := h.m.Get(c.Request.Context(), a, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, toDTO(j, h.ownerNames(c, []*job.Job{j})))
}

// cancel POST /jobs/:id/cancel
func (h *handler) cancel(c *gin.Context) { h.act(c, h.m.Cancel) }

// retry POST /jobs/:id/retry（仅失败任务）
func (h *handler) retry(c *gin.Context) { h.act(c, h.m.Retry) }

// remove DELETE /jobs/:id（仅已结束的任务，连同结果文件）
func (h *handler) remove(c *gin.Context) { h.act(c, h.m.Delete) }

func (h *handler) act(c *gin.Context, fn func(context.Context, job.Actor, idgen.ID) error) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := fn(c.Request.Context(), a, id); err != nil {
		h.fail(c, err)
		return
	}
	response.OK(c, nil)
}

// download GET /jobs/:id/download：校验所有者后由后端代理输出（不暴露存储地址、不发签名链接），
// 与文件模块的私有文件下载一致：attachment + nosniff，本地存储走 ServeContent（支持 Range）。
func (h *handler) download(c *gin.Context) {
	a, ok := h.actor(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	j, err := h.m.Downloadable(ctx, a, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	ctype := j.ResultMime
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	hd := c.Writer.Header()
	set := func() {
		hd.Set("Content-Type", ctype)
		hd.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": j.ResultName}))
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Cache-Control", "no-store")
	}
	st := h.m.Storage()
	if op, ok := st.(storage.Opener); ok {
		f, info, err := op.Open(ctx, j.ResultFile)
		if err != nil {
			response.Fail(c, response.NotFound("error.not_found").Wrap(err))
			return
		}
		defer f.Close()
		set()
		http.ServeContent(c.Writer, c.Request, j.ResultName, info.LastModified, f)
		return
	}
	obj, err := st.Get(ctx, j.ResultFile, nil)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			response.Fail(c, response.NotFound("error.not_found"))
		} else {
			response.Fail(c, err)
		}
		return
	}
	defer obj.Body.Close()
	set()
	hd.Set("Content-Length", strconv.FormatInt(obj.Length, 10))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, obj.Body)
}
