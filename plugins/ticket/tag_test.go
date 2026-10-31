package ticket

import "testing"

func TestNormalizeTags(t *testing.T) {
	got, err := normalizeTags([]string{" vip ", "refund", "vip", "", "紧急"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "vip" || got[1] != "refund" || got[2] != "紧急" {
		t.Fatalf("unexpected tags: %+v", got)
	}
	if _, err := normalizeTags([]string{"a,b"}); err == nil {
		t.Fatal("comma in tag accepted")
	}
	long := make([]rune, 21)
	for i := range long {
		long[i] = '标'
	}
	if _, err := normalizeTags([]string{string(long)}); err == nil {
		t.Fatal("overlong tag accepted")
	}
	tooMany := make([]string, maxTags+1)
	for i := range tooMany {
		tooMany[i] = string(rune('a' + i))
	}
	if _, err := normalizeTags(tooMany); err == nil {
		t.Fatal("too many tags accepted")
	}
}

func TestCanonicalAndSplitTags(t *testing.T) {
	if canonicalTags(nil) != "" {
		t.Fatal("empty tags should store empty string")
	}
	if got := canonicalTags([]string{"vip", "refund"}); got != ",vip,refund," {
		t.Fatalf("canonical tags = %q", got)
	}
	got := splitTags(",vip,refund,")
	if len(got) != 2 || got[0] != "vip" || got[1] != "refund" {
		t.Fatalf("split tags = %+v", got)
	}
}
