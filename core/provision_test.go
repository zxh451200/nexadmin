package core

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"nexadmin/pkg/idgen"
)

func TestRoleAssignmentObserversRunSynchronouslyAndUnregister(t *testing.T) {
	calls := 0
	unregister := RegisterRoleAssignmentObserver(func(_ context.Context, _ *gorm.DB, tenantID, userID idgen.ID, previous, roles []idgen.ID) error {
		calls++
		if tenantID != 2 || userID != 3 || len(previous) != 1 || previous[0] != 4 || len(roles) != 1 || roles[0] != 5 {
			t.Errorf("unexpected observer input: tenant=%d user=%d previous=%v roles=%v", tenantID, userID, previous, roles)
		}
		return nil
	})
	if err := NotifyRoleAssignmentsReplaced(context.Background(), nil, 2, 3, []idgen.ID{4}, []idgen.ID{5}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("observer must run before notification returns: calls=%d", calls)
	}
	unregister()
	unregister()
	if err := NotifyRoleAssignmentsReplaced(context.Background(), nil, 2, 3, []idgen.ID{4}, []idgen.ID{5}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("unregistered observer still ran: calls=%d", calls)
	}
}

func TestRoleAssignmentObserverErrorPropagates(t *testing.T) {
	want := errors.New("cleanup failed")
	unregister := RegisterRoleAssignmentObserver(func(context.Context, *gorm.DB, idgen.ID, idgen.ID, []idgen.ID, []idgen.ID) error {
		return want
	})
	defer unregister()
	if err := NotifyRoleAssignmentsReplaced(context.Background(), nil, 1, 2, nil, nil); !errors.Is(err, want) {
		t.Fatalf("observer error must reach the transaction: %v", err)
	}
}
