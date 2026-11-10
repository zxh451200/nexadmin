package devicehub

// WS 主题授权：「知道设备号」不等于「能看」。核心侧只负责问守卫，判定在这里做
// （见 core/ws/guard.go 与 plugins/im、plugins/ticket 的同款守卫）。
//
// 归属口径与本插件既有模型一致：设备是平台级数据（plg_devhub_* 无租户列，坐席池按
// devicehub:list 共享，见 notify.go 的 queryDevicehubHolders），所以准入条件是
// 「该主体持有 devicehub:list」，与 HTTP 侧 core.Perm("devicehub:list") 同一条线。
// 任何一步取不到证据都按拒绝处理（fail closed）。

import (
	"context"
	"strings"

	"nexadmin/core/auth"
	"nexadmin/core/tenant"
	"nexadmin/core/ws"
)

// wsTopicPrefix / wsTopicFeed 与发布侧（service.go 的 ws.Publish）保持一致，改动要三处同步。
const (
	wsTopicPrefix = "device:"
	wsTopicFeed   = "device:feed"
)

// registerWSTopicGuard 登记 devicehub: 前缀的订阅判定：匿名一律拒绝；非平台调用方一律拒绝
// （与 HTTP 侧 RequirePlatformFeature 同一条线——设备台账是平台共享表，多租户下不能因为
// 持有 devicehub:list 就能收到别人的读数帧）；单台设备必须存在；主体必须能查看设备列表。
// device:feed 是平台级列表主题，只验门槛与权限不验设备存在性。
func registerWSTopicGuard(svc *Service, authSvc *auth.Service) {
	ws.AddTopicGuard(func(cl *auth.Claims, topic string) (bool, bool) {
		if !strings.HasPrefix(topic, wsTopicPrefix) {
			return false, false
		}
		if cl == nil {
			return true, false
		}
		// 平台判据走 tenant.IsPlatformCallerID 单源，与 core.IsPlatformCaller 同口径。
		if !tenant.IsPlatformCallerID(cl.TenantID, cl.Super) {
			return true, false
		}
		if no := strings.TrimPrefix(topic, wsTopicPrefix); no != "feed" {
			if !svc.deviceExists(tenant.Skip(context.Background()), no) {
				return true, false
			}
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
		return true, auth.HasPerm(u.Perms, "devicehub:list")
	})
}

// deviceExists 轻量存在性检查：设备号全局唯一，跳过租户条件查存在即可。
func (s *Service) deviceExists(ctx context.Context, deviceNo string) bool {
	if deviceNo == "" {
		return false
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&Device{}).Where("device_no = ?", deviceNo).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}
