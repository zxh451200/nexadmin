package website

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 商品评价的后台接口。公开站不提供写入口（匿名官网无登录态），
// 评价由运营录入真实回访内容，因此读接口挂 website:product，写接口同样挂该权限。

// GET /plugins/website/reviews?productId=
// 不给 productId 时返回全部（后台评价总表）；给了就只看这一款。
func (h *handler) listReviews(c *gin.Context) {
	pid, _ := strconv.ParseInt(c.Query("productId"), 10, 64)
	rows, err := h.svc.AdminListReviews(c.Request.Context(), pid)
	respond(c, rows, err)
}

// POST /plugins/website/reviews
func (h *handler) createReview(c *gin.Context) {
	var f ReviewForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.CreateReview(c.Request.Context(), f)
	respond(c, row, err)
}

// PUT /plugins/website/reviews/:id
func (h *handler) updateReview(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var f ReviewForm
	if !response.BindJSON(c, &f) {
		return
	}
	row, err := h.svc.UpdateReview(c.Request.Context(), int64(id), f)
	respond(c, row, err)
}

// DELETE /plugins/website/reviews/:id
func (h *handler) deleteReview(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	respond(c, nil, h.svc.DeleteReview(c.Request.Context(), int64(id)))
}
