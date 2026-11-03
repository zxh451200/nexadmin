package ticket

// 数据范围：工单按 assignee_id 归属坐席，复用 core/datascope 的 UserIDCond（与 CRM 同一条线）。
// 约定与框架一致——HTTP 入口先解析范围并挂到请求 ctx 上；ctx 里没有范围表示不裁剪，
// 只留给内部调用方（访客公开接口、邮件进单、超时清扫），它们本来就不该按坐席裁剪。

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/auth"
	"nexadmin/core/datascope"
	"nexadmin/core/response"
)

type scopeKey struct{}

// scopeFrom 取请求者的数据范围；未挂范围返回 nil，表示不裁剪。
func scopeFrom(ctx context.Context) *datascope.Scope {
	sc, _ := ctx.Value(scopeKey{}).(*datascope.Scope)
	return sc
}

// resolveScope 解析登录主体的数据范围并注入请求上下文，供 ticketFilter 与 inScope 共用。
// 开放平台应用主体没有用户身份，FromClaims 按「全部」处理，与框架其它模块同口径。
func (h *handler) resolveScope(c *gin.Context) {
	cl := auth.ClaimsFrom(c)
	if cl == nil {
		c.Next()
		return
	}
	sc, err := datascope.FromClaims(c.Request.Context(), h.svc.db, cl)
	if err != nil {
		response.Fail(c, err)
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), scopeKey{}, sc))
	c.Next()
}

// scopeCond 把范围落成「assignee_id 可见」条件。未派发（assignee_id = 0）必须放行：
// 否则受限坐席眼里的待认领队列整体消失，认领入口随之失效。
func scopeCond(ctx context.Context, db *gorm.DB) *gorm.DB {
	sc := scopeFrom(ctx)
	if sc == nil {
		return db
	}
	cond, args, limited := sc.UserIDCond("assignee_id")
	if !limited {
		return db
	}
	return db.Where("assignee_id = 0 OR ("+cond+")", args...)
}

// ticketVisible 判断单张工单是否在请求者范围内。
func (s *Service) ticketVisible(ctx context.Context, ticketNo string, sc *datascope.Scope) (bool, error) {
	ticketNo = strings.TrimSpace(ticketNo)
	if ticketNo == "" {
		return false, nil
	}
	q := s.db.WithContext(ctx).Model(&Ticket{}).Where("ticket_no = ?", ticketNo)
	if cond, args, limited := sc.UserIDCond("assignee_id"); limited {
		q = q.Where("assignee_id = 0 OR ("+cond+")", args...)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// guardNo 给「中间件挡不到」的入口用：批量操作的一串单号、合并与关联的对侧单号都不在
// :no 路径参数里，所以只能在校验放在服务层。ctx 未挂范围（内部调用）时放行。
func (s *Service) guardNo(ctx context.Context, ticketNo string) error {
	sc := scopeFrom(ctx)
	if sc == nil || !sc.Limited() {
		return nil
	}
	ok, err := s.ticketVisible(ctx, ticketNo, sc)
	if err != nil {
		return err
	}
	if !ok {
		return response.NotFound("ticket.not_found")
	}
	return nil
}

// subscribableTicket 给 WS 守卫用：该主体能否订阅这张单的实时流。
// 守卫拿不到 gin 上下文，所以范围在这里现算；租户要从 claims 补进 ctx，
// 否则 datascope 解析角色时连不上租户（sys_role 带 tenant_id）。
func (s *Service) subscribableTicket(ctx context.Context, ticketNo string, cl *auth.Claims) (bool, error) {
	sc, err := datascope.FromClaims(ctx, s.db, cl)
	if err != nil {
		return false, err
	}
	return s.ticketVisible(ctx, ticketNo, sc)
}

// inScope 挂在 /tickets/:no/... 上：范围外与不存在回同一个 404，避免用工单号探测别人的单。
// 只裁列表不裁单条等于没裁——直接猜单号就能读到全文，所以每条按号读写的路由都要过这里。
func (h *handler) inScope(c *gin.Context) {
	sc := scopeFrom(c.Request.Context())
	if sc == nil || !sc.Limited() {
		c.Next()
		return
	}
	ok, err := h.svc.ticketVisible(c.Request.Context(), c.Param("no"), sc)
	if err != nil {
		response.Fail(c, err)
		c.Abort()
		return
	}
	if !ok {
		response.Fail(c, response.NotFound("ticket.not_found"))
		c.Abort()
		return
	}
	c.Next()
}
