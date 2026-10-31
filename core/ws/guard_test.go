package ws

import (
	"strings"
	"testing"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// 默认策略与守卫优先级：守卫认领了主题就以它的结论为准，包括拒绝。
func TestAllowTopicDefaultPolicy(t *testing.T) {
	agent := &auth.Claims{UserID: idgen.ID(1), TenantID: idgen.ID(2), Username: "agent"}

	if AllowTopic(nil, "public:announcement") != true {
		t.Error("anonymous may subscribe public: topics")
	}
	if AllowTopic(nil, "im:IM123") {
		t.Error("anonymous must not subscribe business topics")
	}
	if AllowTopic(agent, "report:screen:1") != true {
		t.Error("logged-in clients can subscribe topics no guard claims")
	}
	if AllowTopic(nil, "") || AllowTopic(agent, "") {
		t.Error("empty topic is never allowed")
	}
}

func TestAllowTopicGuardDecides(t *testing.T) {
	AddTopicGuard(func(cl *auth.Claims, topic string) (bool, bool) {
		if !strings.HasPrefix(topic, "im:") {
			return false, false
		}
		// 只有会话号以 "1" 结尾的允许，纯做判定优先级的验证。
		return true, strings.HasSuffix(topic, "1")
	})

	if AllowTopic(nil, "im:IM1") != true {
		t.Error("guard verdict must win over the anonymous default")
	}
	if AllowTopic(&auth.Claims{UserID: idgen.ID(5)}, "im:IM2") {
		t.Error("guard denial must win over the logged-in default")
	}
	// 守卫没认领的主题仍走默认策略。
	if !AllowTopic(&auth.Claims{UserID: idgen.ID(5)}, "other:x") {
		t.Error("unclaimed topic should fall back to the default policy")
	}
	if AllowTopic(nil, "other:x") {
		t.Error("unclaimed topic should still require a principal")
	}
}
