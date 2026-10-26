package website

import (
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 管理端接口。路由在 plugin.go 注册；这里只做参数绑定与调用服务层。

type handler struct{ svc *Service }

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

func parseID(c *gin.Context) (idgen.ID, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return 0, false
	}
	return idgen.ID(id), true
}

// ---- 站点设置 ----

// GET /plugins/website/settings
func (h *handler) getSettings(c *gin.Context) {
	row, err := h.svc.Settings(c.Request.Context())
	respond(c, row, err)
}

// PUT /plugins/website/settings
func (h *handler) saveSettings(c *gin.Context) {
	var f SettingForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveSettings(c.Request.Context(), f)
	respond(c, row, err)
}

// ---- 导航 ----

// GET /plugins/website/navs
func (h *handler) listNavs(c *gin.Context) {
	rows, err := h.svc.ListNavs(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/website/navs
func (h *handler) createNav(c *gin.Context) {
	var f ItemForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveNav(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/navs/:id
func (h *handler) updateNav(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f ItemForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveNav(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/navs/:id
func (h *handler) deleteNav(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteNav(c.Request.Context(), id))
}

// ---- 特性卡片 ----

// GET /plugins/website/features
func (h *handler) listFeatures(c *gin.Context) {
	rows, err := h.svc.ListFeatures(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/website/features
func (h *handler) createFeature(c *gin.Context) {
	var f FeatureForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveFeature(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/features/:id
func (h *handler) updateFeature(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f FeatureForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveFeature(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/features/:id
func (h *handler) deleteFeature(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteFeature(c.Request.Context(), id))
}

// ---- 页面 ----

func contentQuery(c *gin.Context) ContentQuery {
	page, size := response.ParsePage(c)
	return ContentQuery{Keyword: c.Query("keyword"), Status: c.Query("status"), Page: page, PageSize: size}
}

// GET /plugins/website/pages?keyword=&status=&page=&pageSize=
func (h *handler) listPages(c *gin.Context) {
	q := contentQuery(c)
	list, total, err := h.svc.ListPages(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// POST /plugins/website/pages
func (h *handler) createPage(c *gin.Context) {
	var f PageForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SavePage(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/pages/:id
func (h *handler) updatePage(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f PageForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SavePage(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/pages/:id
func (h *handler) deletePage(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeletePage(c.Request.Context(), id))
}

// ---- 新闻 ----

// GET /plugins/website/posts?keyword=&status=&page=&pageSize=
func (h *handler) listPosts(c *gin.Context) {
	q := contentQuery(c)
	list, total, err := h.svc.ListPosts(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// POST /plugins/website/posts
func (h *handler) createPost(c *gin.Context) {
	var f PostForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SavePost(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/posts/:id
func (h *handler) updatePost(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f PostForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SavePost(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/posts/:id
func (h *handler) deletePost(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeletePost(c.Request.Context(), id))
}

// ---- 媒体（logo / 封面上传） ----

// POST /plugins/website/media —— multipart 字段 file；返回 {url, mime, size}。
// 上传后得到 /website-assets/media/<id>，写入站点设置的 logoUrl / 封面字段即可。
func (h *handler) uploadMedia(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").WithMsg("缺少 file 字段"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	defer f.Close()
	// SaveMedia 内有 2MB 上限校验；这里先按上限截断读取，防止超大文件整读进内存。
	data := make([]byte, mediaMaxBytes+1)
	n, _ := io.ReadFull(f, data)
	data = data[:n]
	m, err := h.svc.SaveMedia(c.Request.Context(), data)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"url": "/website-assets/media/" + m.ID.String(), "mime": m.Mime, "size": m.Size,
	})
}
