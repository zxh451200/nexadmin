package message

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// Handler 是消息的 HTTP 层。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func uidOf(c *gin.Context) idgen.ID { return auth.ClaimsFrom(c).UserID }

func done(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}

// List GET /messages?unread=1&category=&keyword=&from=2026-10-01&to=2026-10-31
func (h *Handler) List(c *gin.Context) {
	page, size := response.ParsePage(c)
	q := ListQuery{Unread: c.Query("unread") == "1" || c.Query("unread") == "true", Category: response.ParseList(c.Query("category"), response.MaxFilterValues),
		Keyword: strings.TrimSpace(c.Query("keyword")), From: strings.TrimSpace(c.Query("from")), To: strings.TrimSpace(c.Query("to")),
		Page: page, PageSize: size}
	list, total, err := h.svc.List(c.Request.Context(), uidOf(c), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// UnreadCount GET /messages/unread-count
func (h *Handler) UnreadCount(c *gin.Context) {
	cnt, err := h.svc.Unread(c.Request.Context(), uidOf(c))
	done(c, cnt, err)
}

// ReadAll PUT /messages/read-all
func (h *Handler) ReadAll(c *gin.Context) {
	done(c, nil, h.svc.ReadAll(c.Request.Context(), uidOf(c)))
}

// MarkRead PUT /messages/:id/read
func (h *Handler) MarkRead(c *gin.Context) {
	if id, err := idgen.Parse(c.Param("id")); err == nil {
		done(c, nil, h.svc.MarkRead(c.Request.Context(), uidOf(c), id))
		return
	}
	response.Fail(c, response.BadRequest("error.bad_request"))
}

// ClearOrDeleteBatch DELETE /messages?ids=1,2,3 —— 带 ids 就批量删这几条；不带 ids 保持原语义＝清空全部。
// 带了 ids 却解析不出合法 ID 一律 400：否则会静默退化成「清空全部」，那是最难发现的一种误删。
func (h *Handler) ClearOrDeleteBatch(c *gin.Context) {
	uid := uidOf(c)
	raw, withIDs := c.GetQuery("ids")
	if !withIDs {
		done(c, nil, h.svc.ClearAll(c.Request.Context(), uid))
		return
	}
	ids, err := parseIDs(raw)
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteMany(c.Request.Context(), uid, ids)
	done(c, map[string]int64{"deleted": n}, err)
}

// parseIDs 解析逗号分隔的 ID 列表；任一项非法就整体拒绝，不做「删掉能删的」。
func parseIDs(raw string) ([]idgen.ID, error) {
	var ids []idgen.ID
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil || id <= 0 {
			return nil, response.BadRequest("error.bad_request")
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, response.BadRequest("error.bad_request")
	}
	return ids, nil
}

// Delete DELETE /messages/:id
func (h *Handler) Delete(c *gin.Context) {
	if id, err := idgen.Parse(c.Param("id")); err == nil {
		done(c, nil, h.svc.Delete(c.Request.Context(), uidOf(c), id))
		return
	}
	response.Fail(c, response.BadRequest("error.bad_request"))
}
