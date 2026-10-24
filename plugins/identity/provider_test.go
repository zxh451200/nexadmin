package identity

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"nexadmin/core/auth"
	"nexadmin/core/cache"
	"nexadmin/core/config"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
)

// fakeProvider 按用户模块 ResolveExternalUser 的规则建模（modules/system/user/ext_login.go）：
// 先按 (方式, Subject) 找已绑定账号；其次（允许时）匹配同名账号，超管永不匹配；再次（允许时）自动建号，受租户用户数额度限制。
// 真实实现在内置模块里，插件不能 import；这里验证的是插件交给 core 的身份与开关是否正确。
type fakeProvider struct {
	mu     sync.Mutex
	users  map[string]*fakeUser // tenant:username
	bound  map[string]string    // tenant:method:subject -> username
	quota  map[idgen.ID]int
	events []auth.LoginEvent
	nextID idgen.ID
	perms  map[string][]string // 用户名 -> 权限码
}

type fakeUser struct {
	id     idgen.ID
	name   string
	tenant idgen.ID
	super  bool
	roles  []string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{users: map[string]*fakeUser{}, bound: map[string]string{}, quota: map[idgen.ID]int{}, nextID: 100, perms: map[string][]string{}}
}

func ukey(tid idgen.ID, name string) string { return tid.String() + ":" + name }

func (p *fakeProvider) add(tid idgen.ID, name string, super bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	p.users[ukey(tid, name)] = &fakeUser{id: p.nextID, name: name, tenant: tid, super: super}
}

func (p *fakeProvider) Login(context.Context, auth.LoginRequest) (*auth.User, error) {
	return nil, response.BadRequest("auth.invalid_credentials")
}

func (p *fakeProvider) Profile(_ context.Context, id idgen.ID) (*auth.User, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.id == id {
			return &auth.User{ID: u.id, Username: u.name, TenantID: u.tenant, Super: u.super, Roles: u.roles, Perms: append([]string{}, p.perms[u.name]...)}, nil
		}
	}
	return nil, response.Unauthorized("error.unauthorized")
}

var errDenied = response.Forbidden("auth.login_denied")

func (p *fakeProvider) ResolveExternalUser(ctx context.Context, method string, ext auth.ExternalIdentity) (*auth.User, error) {
	tid, ok := tenant.From(ctx)
	if !ok {
		return nil, errDenied
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	to := func(u *fakeUser) *auth.User {
		return &auth.User{ID: u.id, Username: u.name, TenantID: u.tenant, Super: u.super, Roles: u.roles, LoginMethod: method, Perms: []string{}}
	}
	bk := tid.String() + ":" + method + ":" + ext.Subject
	if name, ok := p.bound[bk]; ok {
		return to(p.users[ukey(tid, name)]), nil
	}
	if u := p.users[ukey(tid, ext.Username)]; u != nil && ext.MatchExisting {
		if u.super {
			return nil, errDenied
		}
		p.bound[bk] = u.name
		return to(u), nil
	}
	if ext.AutoCreate && p.users[ukey(tid, ext.Username)] == nil {
		n := 0
		for _, u := range p.users {
			if u.tenant == tid {
				n++
			}
		}
		if lim, has := p.quota[tid]; has && n >= lim {
			return nil, response.Forbidden("tenant.user_quota")
		}
		p.nextID++
		role := ext.DefaultRole
		if role == "" {
			role = "guest"
		}
		u := &fakeUser{id: p.nextID, name: ext.Username, tenant: tid, roles: []string{role}}
		p.users[ukey(tid, ext.Username)] = u
		p.bound[bk] = u.name
		return to(u), nil
	}
	return nil, errDenied
}

func (p *fakeProvider) RecordLogin(_ context.Context, ev auth.LoginEvent) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
}

func (p *fakeProvider) loginLog() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		out = append(out, e.Method+"/"+e.Reason)
	}
	return strings.Join(out, ",")
}

// newAuthService 构造真实的 auth.Service（内存缓存，真实 JWT 签发）并装上 fakeProvider。
func newAuthService(t *testing.T) (*auth.Service, *fakeProvider, cache.Cache) {
	t.Helper()
	ch, err := cache.New(config.Cache{Driver: "memory"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = strings.Repeat("s", 40)
	cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL = time.Minute, time.Hour
	svc, err := auth.NewService(cfg, ch)
	if err != nil {
		t.Fatal(err)
	}
	p := newFakeProvider()
	svc.SetProvider(p)
	return svc, p, ch
}
