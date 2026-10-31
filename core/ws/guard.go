package ws

import (
	"strings"
	"sync"

	"nexadmin/core/auth"
)

// TopicGuard 判定一条连接能否订阅某个主题。handled=false 表示这个守卫不认识该主题
// （交还给下一个守卫）；handled=true 时 allowed 就是结论。
//
// 主题字符串来自客户端，框架无法凭空知道它的归属语义，所以由各业务模块在注册路由时
// 登记自己那一段前缀的判定逻辑（见 plugins/im 的 im: 守卫）。
type TopicGuard func(cl *auth.Claims, topic string) (handled, allowed bool)

var (
	guardMu sync.RWMutex
	guards  []TopicGuard
)

// AddTopicGuard 登记守卫。只在启动/挂载阶段调用；重复登记是安全的（判定是幂等的），
// 但先登记的守卫优先给出结论。
func AddTopicGuard(g TopicGuard) {
	if g == nil {
		return
	}
	guardMu.Lock()
	guards = append(guards, g)
	guardMu.Unlock()
}

// AllowTopic 是所有守卫都没结论时的默认策略：匿名连接只能订阅 public: 前缀的公开主题，
// 已登录连接可以订阅未被守卫认领的主题（守卫认领后即以守卫结论为准，包括拒绝）。
func AllowTopic(cl *auth.Claims, topic string) bool {
	if topic == "" {
		return false
	}
	guardMu.RLock()
	defer guardMu.RUnlock()
	for _, g := range guards {
		if handled, allowed := g(cl, topic); handled {
			return allowed
		}
	}
	return cl != nil || strings.HasPrefix(topic, "public:")
}
