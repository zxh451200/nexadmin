package vrange

import "testing"

func TestSatisfies(t *testing.T) {
	for _, c := range []struct {
		v, r string
		ok   bool
	}{
		{"1.2.3", ">=1.0.0", true}, {"1.2.3", ">=1.3.0", false}, {"1.2.3", ">=1.0.0 <2.0.0", true},
		{"2.0.0", ">=1.0.0, <2.0.0", false}, {"1.2.3", "^1.2.0", true}, {"2.0.0", "^1.2.0", false},
		{"0.2.5", "^0.2.1", true}, {"0.3.0", "^0.2.1", false}, {"1.2.9", "~1.2.0", true}, {"1.3.0", "~1.2.0", false},
		{"1.2.3", "1.2.3", true}, {"v1.2.3-rc1", "=1.2.3", true},
	} {
		got, err := Satisfies(c.v, c.r)
		if err != nil || got != c.ok {
			t.Errorf("Satisfies(%q,%q) = %v, %v; want %v", c.v, c.r, got, err, c.ok)
		}
	}
	for _, bad := range [][2]string{{"1.0.0", "abc"}, {"1.0.0", ""}, {"dev", ">=1.0.0"}} {
		if _, err := Satisfies(bad[0], bad[1]); err == nil {
			t.Errorf("Satisfies(%q,%q) should error", bad[0], bad[1])
		}
	}
}
