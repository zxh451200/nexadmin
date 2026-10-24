package job

import (
	"context"
	"testing"

	"nexadmin/core/auth"
	"nexadmin/pkg/idgen"
)

// 处理器拿到的 context 带有归属用户（提交者 + "job:<type>"），但不是可通过校验的登录态。
func TestHandlerContextCarriesOwner(t *testing.T) {
	st := newMemStore()
	m := newMgr(t, st, "n1", nil, nil)
	var (
		gotID   idgen.ID
		gotName string
		gotOK   bool
		claims  *auth.Claims
	)
	typ := regType(t, func(ctx context.Context, j *Job, report Report) (ResultFile, error) {
		gotID, gotName, gotOK = auth.UserFromContext(ctx)
		claims = auth.ClaimsFromContext(ctx)
		return ResultFile{}, nil
	})
	if _, err := m.Submit(tctx(), Submit{Type: typ, Owner: 7}); err != nil {
		t.Fatal(err)
	}
	if !m.RunOnce() {
		t.Fatal("RunOnce did nothing")
	}
	if !gotOK || gotID != 7 || gotName != "job:"+typ {
		t.Fatalf("归属用户 = %v %d %q，期望 7 job:%s", gotOK, gotID, gotName, typ)
	}
	if claims != nil {
		t.Fatalf("不应伪造 Claims：%+v", claims)
	}
}
