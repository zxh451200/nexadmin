package message

import (
	"context"
	"testing"
	"time"

	"nexadmin/core/cache"
	"nexadmin/core/cluster"
	"nexadmin/core/config"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

func newHub(c cache.Cache, id string) (*hub, *cluster.Bus) {
	h := &hub{subs: map[hubKey]map[chan struct{}]struct{}{}}
	b := cluster.NewWithID(c, nil, id)
	h.attach(b)
	return h, b
}

func drain(ch chan struct{}, wait time.Duration) int {
	n := 0
	deadline := time.After(wait)
	for {
		select {
		case <-ch:
			n++
		case <-deadline:
			return n
		}
	}
}

// 实例 A 的 poke：A 的连接收到一次（本地），B 的连接收到一次（广播），A 不会因为订阅回环收到第二次。
func TestPokeReachesOtherInstanceExactlyOnce(t *testing.T) {
	c, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ha, ba := newHub(c, "A")
	hb, bb := newHub(c, "B")
	defer ba.Close()
	defer bb.Close()

	ctx := tenant.With(context.Background(), 1)
	uid := idgen.ID(7)
	chA, cancelA := ha.subscribe(keyOf(ctx, uid))
	defer cancelA()
	chB, cancelB := hb.subscribe(keyOf(ctx, uid))
	defer cancelB()
	chOther, cancelO := hb.subscribe(keyOf(ctx, 8)) // 别的用户不应被唤醒
	defer cancelO()
	chOtherTenant, cancelT := hb.subscribe(keyOf(tenant.With(context.Background(), 2), uid)) // 别的租户同 uid 也不应被唤醒
	defer cancelT()

	ha.poke(ctx, uid)

	if n := drain(chB, 300*time.Millisecond); n != 1 {
		t.Fatalf("remote instance connection woke %d times, want 1", n)
	}
	if n := drain(chA, 100*time.Millisecond); n != 1 {
		t.Fatalf("local connection woke %d times, want exactly 1 (no echo)", n)
	}
	if n := drain(chOther, 50*time.Millisecond); n != 0 {
		t.Fatalf("other user woke %d times", n)
	}
	if n := drain(chOtherTenant, 50*time.Millisecond); n != 0 {
		t.Fatalf("other tenant woke %d times", n)
	}
}

// 没有总线（单实例 / 手工构造的 Deps）：只推本地，行为同改动前。
func TestPokeLocalOnlyWithoutBus(t *testing.T) {
	h := &hub{subs: map[hubKey]map[chan struct{}]struct{}{}}
	h.attach(nil)
	ctx := tenant.With(context.Background(), 1)
	ch, cancel := h.subscribe(keyOf(ctx, 7))
	defer cancel()
	h.poke(ctx, 7)
	h.poke(ctx, 7) // 合并：容量 1
	if n := drain(ch, 50*time.Millisecond); n != 1 {
		t.Fatalf("woke %d times, want 1", n)
	}
}

// 订阅断线重连后全员重算一次。
func TestPokeAllWakesEveryConnection(t *testing.T) {
	h := &hub{subs: map[hubKey]map[chan struct{}]struct{}{}}
	c1, x1 := h.subscribe(hubKey{1, 1})
	c2, x2 := h.subscribe(hubKey{2, 9})
	defer x1()
	defer x2()
	h.pokeAll()
	if drain(c1, 30*time.Millisecond) != 1 || drain(c2, 30*time.Millisecond) != 1 {
		t.Fatal("pokeAll must wake all connections")
	}
}
