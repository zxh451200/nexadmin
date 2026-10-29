package media

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
)

type handler struct{ svc *service }

// Stats GET /plugins/media/stats —— 公开图片总数 / 已有 WebP / 待生成。
func (h *handler) Stats(c *gin.Context) {
	v, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Images GET /plugins/media/images?page=&pageSize= —— 待生成 WebP 的原图。
func (h *handler) Images(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.Pending(c.Request.Context(), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// VideoConfig GET /plugins/media/video/config —— 令牌只回「有没有」。
func (h *handler) VideoConfig(c *gin.Context) {
	cfg, err := h.svc.VideoConfig(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view(cfg))
}

// SaveVideoConfig PUT /plugins/media/video/config
func (h *handler) SaveVideoConfig(c *gin.Context) {
	var in configReq
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveVideoConfig(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// CreateVideoUpload POST /plugins/media/video/upload —— 申请一次性直传地址。
func (h *handler) CreateVideoUpload(c *gin.Context) {
	var in videoUploadReq
	if !response.BindJSON(c, &in) {
		return
	}
	uploadURL, videoID, playback, err := h.svc.CreateVideoUpload(c.Request.Context(), in.Name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"uploadUrl": uploadURL, "videoId": videoID, "playbackUrl": playback})
}

// SaveVideo POST /plugins/media/video/assets —— 直传完成后登记台账。
func (h *handler) SaveVideo(c *gin.Context) {
	var in videoSaveReq
	if !response.BindJSON(c, &in) {
		return
	}
	row, err := h.svc.SaveVideo(c.Request.Context(), usernameOf(c), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, row)
}

// Videos GET /plugins/media/video/assets?page=&pageSize=
func (h *handler) Videos(c *gin.Context) {
	page, size := response.ParsePage(c)
	list, total, err := h.svc.Videos(c.Request.Context(), page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// DeleteVideo DELETE /plugins/media/video/assets/:id
func (h *handler) DeleteVideo(c *gin.Context) {
	if err := h.svc.DeleteVideo(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

func usernameOf(c *gin.Context) string {
	if cl := auth.ClaimsFrom(c); cl != nil {
		return cl.Username
	}
	return ""
}
