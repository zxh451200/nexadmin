package tenant

import "testing"

func TestValidLogoURL(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", true},
		{"/uploads/logo.png", true},
		{"/icon.svg", true},
		{"https://cdn.example.com/logo.png", true},
		{"http://cdn.example.com/logo.png?v=2", true},
		{"//evil.com/logo.png", false},
		{"javascript:alert(1)", false},
		{"data:image/png;base64,AAAA", false},
		{"ftp://example.com/logo.png", false},
		{"uploads/logo.png", false},
		{"https://", false},
	}
	for _, c := range cases {
		if got := validLogoURL(c.in); got != c.ok {
			t.Errorf("validLogoURL(%q) = %v, want %v", c.in, got, c.ok)
		}
	}
}
