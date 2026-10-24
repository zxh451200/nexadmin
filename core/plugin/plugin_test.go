package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

func signed(t *testing.T, priv ed25519.PrivateKey, plugin string, expires time.Time) []byte {
	t.Helper()
	raw, err := Sign(priv, License{
		Plugin: plugin, Customer: "acme", IssuedAt: now.Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func status(err error) string {
	var le *LicenseError
	if errors.As(err, &le) {
		return le.Status
	}
	return ""
}

func TestVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	future := now.Add(24 * time.Hour)

	t.Run("valid", func(t *testing.T) {
		l, err := Verify(signed(t, priv, "cron", future), "cron", now, pub)
		if err != nil || l.Customer != "acme" {
			t.Fatalf("got %+v, %v", l, err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		_, err := Verify(signed(t, priv, "cron", now.Add(-time.Second)), "cron", now, pub)
		if status(err) != StatusExpired {
			t.Fatalf("want expired, got %v", err)
		}
	})
	t.Run("wrong plugin", func(t *testing.T) {
		_, err := Verify(signed(t, priv, "other", future), "cron", now, pub)
		if status(err) != StatusInvalid {
			t.Fatalf("want invalid, got %v", err)
		}
	})
	t.Run("tampered payload", func(t *testing.T) {
		var env envelope
		if err := json.Unmarshal(signed(t, priv, "cron", future), &env); err != nil {
			t.Fatal(err)
		}
		forged, _ := json.Marshal(License{Plugin: "cron", Customer: "evil", ExpiresAt: future.AddDate(10, 0, 0).Format(time.RFC3339)})
		env.Payload = base64Std(forged)
		raw, _ := json.Marshal(env)
		if _, err := Verify(raw, "cron", now, pub); status(err) != StatusInvalid {
			t.Fatalf("want invalid, got %v", err)
		}
	})
	t.Run("wrong key", func(t *testing.T) {
		if _, err := Verify(signed(t, priv, "cron", future), "cron", now, otherPub); status(err) != StatusInvalid {
			t.Fatalf("want invalid, got %v", err)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		for _, in := range []string{"", "not json", `{"payload":"!!","signature":"!!"}`} {
			if _, err := Verify([]byte(in), "cron", now, pub); status(err) != StatusInvalid {
				t.Fatalf("%q: want invalid, got %v", in, err)
			}
		}
	})
}

func TestEvaluateAndImport(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	root := t.TempDir()
	mk := func(id, edition string) {
		dir := filepath.Join(root, "plugins", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		y := "id: " + id + "\nname: " + id + "\nversion: 1.0.0\nedition: " + edition + "\n"
		if err := os.WriteFile(filepath.Join(dir, "plugin.yaml"), []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("freebie", "free")
	mk("cron", "pro")
	mk("mismatch", "pro")

	m := newManager([]Plugin{
		{ID: "freebie", Edition: Free, Module: fakeModule{}},
		{ID: "cron", Edition: Pro, Module: fakeModule{}},
		{ID: "mismatch", Edition: Free, Module: fakeModule{}}, // 清单是 pro，注册是 free
		{ID: "nomanifest", Edition: Free, Module: fakeModule{}},
	}, Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})

	got := map[string]string{}
	for _, i := range m.List() {
		got[i.ID] = i.Status
	}
	want := map[string]string{"freebie": StatusActive, "cron": StatusUnlicensed, "mismatch": StatusInvalid, "nomanifest": StatusInvalid}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: want %s, got %s", k, v, got[k])
		}
	}

	// 导入被篡改的授权：拒绝，且不落盘。
	bad := signed(t, priv, "other", now.Add(time.Hour))
	if _, err := m.ImportLicense("cron", string(bad)); status(err) != StatusInvalid {
		t.Fatalf("want invalid, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "licenses", "cron.lic")); !os.IsNotExist(err) {
		t.Fatal("invalid license must not be written")
	}
	if _, err := m.ImportLicense("freebie", "x"); !errors.Is(err, ErrFree) {
		t.Fatalf("want ErrFree, got %v", err)
	}
	if _, err := m.ImportLicense("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// 有效授权：落盘；没有运行时依赖时要求重启；重新评估后为 active。
	good := signed(t, priv, "cron", now.Add(time.Hour))
	res, err := m.ImportLicense("cron", string(good))
	if err != nil || !res.NeedRestart {
		t.Fatalf("got %+v, %v", res, err)
	}
	m2 := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return now }})
	if i := m2.List()[0]; i.Status != StatusActive || i.Customer != "acme" {
		t.Fatalf("got %+v", i)
	}
	// 过期后重新评估为 expired。
	m3 := newManager([]Plugin{{ID: "cron", Edition: Pro, Module: fakeModule{}}},
		Options{Root: root, PublicKey: pub, Now: func() time.Time { return now.Add(2 * time.Hour) }})
	if i := m3.List()[0]; i.Status != StatusExpired {
		t.Fatalf("got %+v", i)
	}
}
