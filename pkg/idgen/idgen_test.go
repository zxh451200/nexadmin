package idgen

import (
	"encoding/json"
	"testing"
)

func TestUniqueAndMonotonic(t *testing.T) {
	g, err := New(7)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[ID]struct{}, 20000)
	var prev ID
	for i := 0; i < 20000; i++ {
		id, err := g.Next()
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %d", id)
		}
		if id <= prev {
			t.Fatalf("not increasing: %d after %d", id, prev)
		}
		seen[id], prev = struct{}{}, id
	}
}

func TestClockRollback(t *testing.T) {
	g, _ := New(1)
	now := epoch + 1000
	g.now = func() int64 { return now }
	if _, err := g.Next(); err != nil {
		t.Fatal(err)
	}
	now -= 100 // 超出 5ms 容忍窗口
	if _, err := g.Next(); err == nil {
		t.Fatal("expected error on large clock rollback")
	}
}

func TestNodeRange(t *testing.T) {
	if _, err := New(MaxNode + 1); err == nil {
		t.Fatal("expected error for node out of range")
	}
}

func TestJSONString(t *testing.T) {
	b, _ := json.Marshal(struct{ ID ID }{ID: 1790000000000000001})
	if string(b) != `{"ID":"1790000000000000001"}` {
		t.Fatalf("unexpected json: %s", b)
	}
	var v struct{ ID ID }
	if err := json.Unmarshal([]byte(`{"ID":"42"}`), &v); err != nil || v.ID != 42 {
		t.Fatalf("unmarshal: %v %v", v, err)
	}
}
