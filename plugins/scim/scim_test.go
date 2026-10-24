package scim

import (
	"testing"
	"time"

	"nexadmin/pkg/idgen"
)

func TestTokenHashIsStableAndNonReversible(t *testing.T) {
	const raw = "nxscim_test-secret"
	if hashToken(raw) == raw || hashToken(raw) != hashToken(raw) {
		t.Fatal("token hash is invalid")
	}
	if hashToken(raw) == hashToken(raw+"x") {
		t.Fatal("different tokens share hash")
	}
}

func TestResourceETagChangesWithUpdatedAt(t *testing.T) {
	id := idgen.ID(1)
	a := userRow{ID: id, UpdatedAt: time.Unix(1, 0)}
	b := a
	b.UpdatedAt = time.Unix(2, 0)
	if etag(a) == etag(b) {
		t.Fatal("etag did not change")
	}
}

func TestStatusMapping(t *testing.T) {
	if status(true) != "1" || status(false) != "0" {
		t.Fatal("invalid status mapping")
	}
}
