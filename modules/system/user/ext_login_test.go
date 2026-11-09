package user

import (
	"testing"

	"nexadmin/core/auth"
)

func TestProfilePatchOnlyTouchesProfileFields(t *testing.T) {
	u := &User{Username: "local-name", Nickname: "旧昵称", Email: "old@example.com", Phone: "13800000000"}
	u.TenantID = 1
	patch, warnings := profilePatch(u, auth.ExternalIdentity{
		Username: "idp-name",
		Nickname: "新昵称",
		Email:    "NEW@Example.com",
		Phone:    "13900000000",
	})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if patch["nickname"] != "新昵称" || patch["email"] != "new@example.com" || patch["phone"] != "13900000000" {
		t.Fatalf("unexpected patch: %v", patch)
	}
	for _, k := range []string{"username", "status", "role", "password"} {
		if _, ok := patch[k]; ok {
			t.Fatalf("profile sync must not write %q", k)
		}
	}
}

func TestProfilePatchIgnoresEmptyAndInvalidValues(t *testing.T) {
	u := &User{Nickname: "本地昵称", Email: "keep@example.com", Phone: "13800000000"}
	patch, warnings := profilePatch(u, auth.ExternalIdentity{Email: "not-an-email", Phone: "abc"})
	if len(patch) != 0 {
		t.Fatalf("empty/invalid values must not produce a patch: %v", patch)
	}
	if len(warnings) != 2 {
		t.Fatalf("want warnings for email and phone, got %v", warnings)
	}
	// 断言里没给的属性不覆盖本地值（空值不是删除）。
	patch, warnings = profilePatch(u, auth.ExternalIdentity{Nickname: "  ", Email: "", Phone: ""})
	if len(patch) != 0 || len(warnings) != 0 {
		t.Fatalf("empty identity must be a no-op, patch=%v warnings=%v", patch, warnings)
	}
}
