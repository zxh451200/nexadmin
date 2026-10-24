package notice

import (
	"strings"
	"testing"
)

func TestSanitizeUsesHTMLSafe(t *testing.T) {
	got := sanitize(`<p onclick="x()">hi<script>alert(1)</script></p><a href="javascript:alert(1)">l</a>`)
	if got != `<p>hi</p><a rel="noopener noreferrer">l</a>` {
		t.Fatalf("unexpected sanitize output: %q", got)
	}
	if strings.Contains(sanitize(`<img src=x onerror=alert(1)>`), "onerror") {
		t.Fatal("event attribute survived")
	}
}
