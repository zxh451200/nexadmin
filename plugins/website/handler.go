package website

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/ai"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// 管理端接口。路由在 plugin.go 注册；这里只做参数绑定与调用服务层。

type handler struct {
	svc *Service
	// gw 是 AI 翻译网关，可为 nil（AI 未配置）；只有机翻接口用到它。
	gw *ai.Gateway
	// previewKey 是草稿预览令牌的签名密钥（复用框架 JWT 密钥）；nil = 预览不可用。
	previewKey []byte
}

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
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, newSettingView(row))
}

// PUT /plugins/website/settings
func (h *handler) saveSettings(c *gin.Context) {
	var f SettingForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveSettings(c.Request.Context(), f)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, newSettingView(row))
}

// ---- 在线留言 ----

// GET /plugins/website/messages?keyword=&status=&page=&pageSize=
func (h *handler) listMessages(c *gin.Context) {
	page, size := response.ParsePage(c)
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !ValidMessageStatus(status) {
		response.Fail(c, response.BadRequest("website.message_status_invalid"))
		return
	}
	q := MessageQuery{Page: page, PageSize: size, Keyword: c.Query("keyword"), Status: status}
	list, total, err := h.svc.ListMessages(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// GET /plugins/website/messages/unread-count —— 未读（new）条数，侧边栏徽标用。
func (h *handler) unreadMessageCount(c *gin.Context) {
	n, err := h.svc.CountNewMessages(c.Request.Context())
	respond(c, map[string]int64{"count": n}, err)
}

// GET /plugins/website/messages/export?keyword=&status= —— 导出当前筛选下的全部留言。
// 返回 JSON 列表（不带分页），CSV 由前端生成，与 /users/export 的默认分支同款形状。
func (h *handler) exportMessages(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !ValidMessageStatus(status) {
		response.Fail(c, response.BadRequest("website.message_status_invalid"))
		return
	}
	rows, err := h.svc.ExportMessages(c.Request.Context(), MessageQuery{Keyword: c.Query("keyword"), Status: status})
	respond(c, rows, err)
}

// PUT /plugins/website/messages/status —— 批量改状态（单条就是长度为 1 的 ids）。
// 返回实际改动条数：并发下有人先删了几条，界面据实刷新而不是报假成功。
func (h *handler) setMessageStatus(c *gin.Context) {
	var f MessageStatusForm
	if !response.BindJSON(c, &f) {
		return
	}
	n, err := h.svc.SetMessagesStatus(c.Request.Context(), f.IDs, strings.TrimSpace(f.Status))
	respond(c, map[string]int64{"updated": n}, err)
}

// DELETE /plugins/website/messages?ids=1,2,3 —— 批量删除（形状与 /users、/tenants 一致）。
func (h *handler) deleteMessages(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteMessages(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// parseIDs 解析逗号分隔的 ID 列表；任一项非法就整体拒绝，不做「删掉能删的」。
func parseIDs(raw string) ([]idgen.ID, error) {
	var ids []idgen.ID
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := idgen.Parse(part)
		if err != nil || id <= 0 {
			return nil, response.BadRequest("website.ids_invalid")
		}
		ids = append(ids, id)
	}
	return ids, nil
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

// PUT /plugins/website/navs/reorder
func (h *handler) reorderNavs(c *gin.Context) {
	var f NavOrderForm
	if !response.BindJSON(c, &f) {
		return
	}
	respond(c, nil, h.svc.ReorderNavs(c.Request.Context(), f.Items))
}

// DELETE /plugins/website/navs/:id
func (h *handler) deleteNav(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteNav(c.Request.Context(), id))
}

// DELETE /plugins/website/navs?ids=1,2,3 —— 批量删除；每项连同后代一起删，返回实际删除条数。
func (h *handler) deleteNavs(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteNavs(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// ---- 特性卡片 ----

// GET /plugins/website/features
func (h *handler) listFeatures(c *gin.Context) {
	rows, err := h.svc.ListFeatures(c.Request.Context())
	respond(c, rows, err)
}

// GET /plugins/website/feature-icons
// 名称 + SVG 内部标记：管理端下拉据此渲染预览，与官网 template icon 函数共用 iconPaths。
func (h *handler) listFeatureIcons(c *gin.Context) {
	out := make([]gin.H, 0, len(featureIconOrder))
	for _, name := range featureIconOrder {
		out = append(out, gin.H{"name": name, "paths": iconPaths[name]})
	}
	response.OK(c, out)
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

// DELETE /plugins/website/features?ids=1,2,3 —— 批量删除（形状与 /pages 一致）。
func (h *handler) deleteFeatures(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteFeatures(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// ---- 页面 ----

func contentQuery(c *gin.Context) ContentQuery {
	page, size := response.ParsePage(c)
	q := ContentQuery{
		Keyword: c.Query("keyword"), Status: c.Query("status"), Missing: c.Query("missing"),
		Page: page, PageSize: size,
	}
	// 分类筛选只对新闻生效；非法值当没有这个筛选，不让一个手改的 URL 500。
	if id, err := idgen.Parse(c.Query("categoryId")); err == nil && id > 0 {
		q.CategoryID = id
	}
	return q
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

// PUT /plugins/website/pages/status —— 批量发布/下线（单条就是长度为 1 的 ids）。
func (h *handler) setPagesStatus(c *gin.Context) {
	var f PostStatusForm
	if !response.BindJSON(c, &f) {
		return
	}
	n, err := h.svc.SetPagesStatus(c.Request.Context(), f.IDs, strings.TrimSpace(f.Status))
	respond(c, map[string]int64{"updated": n}, err)
}

// DELETE /plugins/website/pages?ids=1,2,3 —— 批量删除。
func (h *handler) deletePages(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeletePages(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// GET /plugins/website/pages/:id/preview —— 签发草稿预览链接（previewTTL 内有效）。
// 返回 /p/:slug?preview=<token>：前端新开标签页即可看到未发布页面的真实版式。
func (h *handler) pagePreview(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.svc.PageByID(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	token, err := signPagePreview(h.previewKey, row.Slug, time.Now())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"url": "/p/" + row.Slug + "?preview=" + token})
}

// POST /plugins/website/pages/:id/copy —— 复制为草稿（内容与译文带走，发布态归零）。
func (h *handler) copyPage(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.svc.CopyPage(c.Request.Context(), id)
	respond(c, row, err)
}

// ---- 新闻 ----

// GET /plugins/website/posts?keyword=&status=&categoryId=&page=&pageSize=
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

// PUT /plugins/website/posts/status —— 批量发布/下线（单条就是长度为 1 的 ids）。
// 返回实际改动条数：并发下有人先删了几条，界面据实刷新而不是报假成功。
func (h *handler) setPostStatus(c *gin.Context) {
	var f PostStatusForm
	if !response.BindJSON(c, &f) {
		return
	}
	n, err := h.svc.SetPostsStatus(c.Request.Context(), f.IDs, strings.TrimSpace(f.Status))
	respond(c, map[string]int64{"updated": n}, err)
}

// DELETE /plugins/website/posts?ids=1,2,3 —— 批量删除（形状与 /messages 一致）。
func (h *handler) deletePosts(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeletePosts(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// GET /plugins/website/posts/export?keyword=&status=&categoryId= —— 导出当前筛选下的全部新闻。
// 返回 JSON 列表（不带分页），CSV 由前端生成，与 /messages/export 同款形状。
func (h *handler) exportPosts(c *gin.Context) {
	rows, err := h.svc.ExportPosts(c.Request.Context(), contentQuery(c))
	respond(c, rows, err)
}

// GET /plugins/website/posts/:id/preview —— 签发草稿预览链接（previewTTL 内有效）。
// 返回 /news/:slug?preview=<token>：前端新开标签页即可看到未发布新闻的真实版式。
func (h *handler) postPreview(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.svc.PostByID(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	token, err := signPostPreview(h.previewKey, row.Slug, time.Now())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"url": "/news/" + row.Slug + "?preview=" + token})
}

// POST /plugins/website/posts/:id/copy —— 复制为草稿（内容/译文/分类带走，发布态归零）。
func (h *handler) copyPost(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.svc.CopyPost(c.Request.Context(), id)
	respond(c, row, err)
}

// ---- 新闻分类 ----

// GET /plugins/website/categories
func (h *handler) listCategories(c *gin.Context) {
	rows, err := h.svc.ListCategories(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/website/categories
func (h *handler) createCategory(c *gin.Context) {
	var f CategoryForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveCategory(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/categories/:id
func (h *handler) updateCategory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f CategoryForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveCategory(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/categories/:id —— 连同新闻-分类关联一起删。
func (h *handler) deleteCategory(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteCategory(c.Request.Context(), id))
}

// ---- 新闻标签 ----

// GET /plugins/website/tags —— 带每个标签的新闻数。
func (h *handler) listTags(c *gin.Context) {
	rows, err := h.svc.ListTags(c.Request.Context())
	respond(c, rows, err)
}

// POST /plugins/website/tags
func (h *handler) createTag(c *gin.Context) {
	var f TagForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveTag(c.Request.Context(), 0, f)
	respond(c, row, err)
}

// PUT /plugins/website/tags/:id
func (h *handler) updateTag(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f TagForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.SaveTag(c.Request.Context(), id, f)
	respond(c, row, err)
}

// DELETE /plugins/website/tags/:id —— 连同新闻-标签关联一起删。
func (h *handler) deleteTag(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteTag(c.Request.Context(), id))
}

// ---- 媒体（logo / 封面上传） ----

// GET /plugins/website/media/limits —— 当前生效的图片上传限制。前端的文件选择器与提示
// 文案都按它渲染，省得同一组限制在 Go、TS、语言包里各抄一份。
func (h *handler) mediaLimits(c *gin.Context) {
	response.OK(c, h.svc.MediaLimits(c.Request.Context()))
}

// POST /plugins/website/media —— multipart 字段 file；返回 {url, mime, size}。
// 上传后得到 /website-assets/media/<id>，写入站点设置的 logoUrl / 封面字段即可。
func (h *handler) uploadMedia(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, response.BadRequest("website.file_required"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request"))
		return
	}
	defer f.Close()
	// SaveMedia 内有上限校验；这里先按生效上限截断读取，防止超大文件整读进内存。
	lim := h.svc.MediaLimits(c.Request.Context())
	data := make([]byte, lim.MaxBytes+1)
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
