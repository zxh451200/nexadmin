package im

// WS 主题授权：坐席端订阅的是 `im:<会话号>`，而会话号是可枚举的雪花 ID——
// 「知道号」不等于「能看」。核心侧只负责问守卫，判定在这里做（见 core/ws/guard.go）。
//
// 归属口径与本插件既有模型一致：在线客服的会话池是平台级共享的（见 notify.go 的 queryAgents
// 注释），所以准入条件是「该主体在自己租户内有 im:list」，与 HTTP 侧 core.Perm("im:list") 同一条线。
// 若将来把会话改为按租户隔离，只需在这里补上 conv 与 claims 的租户比对，其余不用动。

import (
	"context"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/core/ws"
)

// wsTopicPrefix 与发布侧的 ws.Publish("im:"+convNo, ...) 保持一致。
const wsTopicPrefix = "im:"

// registerWSTopicGuard 登记 im: 前缀的订阅判定：匿名一律拒绝；会话必须存在；主体必须能查看会话列表。
// 任何一步取不到证据都按拒绝处理（fail closed），而不是让未知状态订阅到别人的会话。
func registerWSTopicGuard(svc *Service, authSvc *auth.Service) {
	ws.AddTopicGuard(func(cl *auth.Claims, topic string) (bool, bool) {
		if !strings.HasPrefix(topic, wsTopicPrefix) {
			return false, false
		}
		convNo := strings.TrimPrefix(topic, wsTopicPrefix)
		if cl == nil || convNo == "" {
			return true, false
		}
		// 会话号全局唯一，查存在性要跳过租户条件。
		if _, err := svc.Conversation(tenant.Skip(context.Background()), convNo); err != nil {
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
		if err != nil {
			return true, false
		}
		return true, auth.HasPerm(u.Perms, "im:list")
	})
}
