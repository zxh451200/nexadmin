package ticket

// WS 主题授权：「知道工单号」不等于「能看」。核心侧只负责问守卫，判定在这里做
// （见 core/ws/guard.go 与 plugins/im 的同款守卫）。
//
// 归属口径与 HTTP 侧保持一致：列表权限（ticket:list）之外还要过一层数据范围
// （见 scope.go），否则守卫就成了整套收口里唯一能被订阅绕过去的洞。
// 工单本身仍是平台级数据（plg_ticket 无租户列），租户只用于解析角色与权限。
// 任何一步取不到证据都按拒绝处理（fail closed）。

import (
	"context"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/core/ws"
)

// registerWSTopicGuard 登记 ticket: 前缀的订阅判定：匿名一律拒绝；单张工单必须存在；
// 主体必须能查看工单列表，且订阅单张工单时该单要在其数据范围内。ticket:feed 是平台级列表
// 主题，只验权限不验工单存在性。
func registerWSTopicGuard(svc *Service, authSvc *auth.Service) {
	ws.AddTopicGuard(func(cl *auth.Claims, topic string) (bool, bool) {
		if !strings.HasPrefix(topic, wsTopicPrefix) {
			return false, false
		}
		if cl == nil {
			return true, false
		}
		no := strings.TrimPrefix(topic, wsTopicPrefix)
		if no != "feed" && !svc.ticketExists(tenant.Skip(context.Background()), no) {
			return true, false
		}
		if cl.Super {
			return true, true
		}
		if authSvc == nil {
			return true, false
		}
		// Profile 按 cl.TenantID 落租户，取的是最新权限，不是签发令牌时的快照。
		u, err := authSvc.Profile(context.Background(), cl)
		if err != nil || !auth.HasPerm(u.Perms, "ticket:list") {
			return true, false
		}
		// 权限之外再收一层数据范围：持 ticket:list 不等于能实时看别人的单。
		// 解析失败或范围外一律拒绝（fail closed），与本文件开头的口径一致。
		if no != "feed" {
			ok, err := svc.subscribableTicket(tenant.With(context.Background(), cl.TenantID), no, cl)
			if err != nil || !ok {
				return true, false
			}
		}
		return true, true
	})
}

// ticketExists 轻量存在性检查：工单号全局唯一，跳过租户条件查存在即可。
func (s *Service) ticketExists(ctx context.Context, ticketNo string) bool {
	if ticketNo == "" {
		return false
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Ticket{}).Where("ticket_no = ?", ticketNo).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}
