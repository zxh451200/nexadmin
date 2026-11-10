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

// UserTopicPrefix 是私有推送主题前缀：PublishToUser 向 "user:<uid>" 投递。
const UserTopicPrefix = "user:"

// AllowTopic 是所有守卫都没结论时的判定：user:<uid> 私有推送只允许订阅自己的
// （core 与宿主业务的私有通知都走 PublishToUser）；public: 前缀对所有人开放；
// 其余未认领主题一律拒绝——「已登录就能订阅」等于把忘了登记守卫的主题向全体
// 用户裸奔（他人的私有推送可被直接订阅），新主题接入必须显式登记守卫（fail closed）。
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
	if strings.HasPrefix(topic, UserTopicPrefix) {
		return cl != nil && cl.UserID.String() == strings.TrimPrefix(topic, UserTopicPrefix)
	}
	return strings.HasPrefix(topic, "public:")
}
