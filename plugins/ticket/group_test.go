package ticket

import "testing"

func TestGroupInputNormalize(t *testing.T) {
	var bad GroupInput
	if err := bad.normalize(); err == nil {
		t.Fatal("empty group name accepted")
	}
	good := GroupInput{Name: "  售后一组  ", Description: "  白天班  ", Sort: -3}
	if err := good.normalize(); err != nil {
		t.Fatal(err)
	}
	if good.Name != "售后一组" || good.Description != "白天班" || good.Sort != 0 {
		t.Fatalf("unexpected normalized input: %+v", good)
	}
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	bad = GroupInput{Name: "x", Description: string(long)}
	if err := bad.normalize(); err == nil {
		t.Fatal("oversized description accepted")
	}
}
