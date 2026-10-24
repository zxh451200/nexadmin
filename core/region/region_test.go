package region

import "testing"

func TestResolveCurrentAndUnknown(t *testing.T) {
	name, historical := Resolve("110000")
	if name != "北京市" || historical {
		t.Fatalf("current = %q, historical=%v", name, historical)
	}
	name, historical = Resolve("999999")
	if name != "999999" || historical {
		t.Fatalf("unknown = %q, historical=%v", name, historical)
	}
}

func TestResolveTrimsCode(t *testing.T) {
	name, historical := Resolve(" 110000 ")
	if name != "北京市" || historical {
		t.Fatalf("trimmed = %q, historical=%v", name, historical)
	}
}

func TestHistoricalInfoHasNoFabricatedData(t *testing.T) {
	if _, ok := HistoricalInfo("110000"); ok {
		t.Fatal("current code must not be treated as historical")
	}
	if got := CurrentCodes("999999"); got != nil {
		t.Fatalf("unknown history = %#v", got)
	}
}
