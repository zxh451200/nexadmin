package testkit

import (
	"testing"

	"nexadmin/core/tenant"
)

func TestContexts(t *testing.T) {
	if got, ok := tenant.From(Context(42)); !ok || got != 42 {
		t.Fatalf("tenant context = %v, %v", got, ok)
	}
	if !tenant.Skipped(PlatformContext()) {
		t.Fatal("platform context is not skipped")
	}
}

func TestMemoryCache(t *testing.T) {
	c := MemoryCache(t)
	if err := c.Set(Context(42), "k", "v", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(Context(42), "k"); err != nil || got != "v" {
		t.Fatalf("cache = %q, %v", got, err)
	}
}
