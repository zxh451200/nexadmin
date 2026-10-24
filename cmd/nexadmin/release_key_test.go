package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseTargetKeys(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub2, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k2 := base64.StdEncoding.EncodeToString(pub2)
	k := base64.StdEncoding.EncodeToString(pub)
	for _, tt := range []struct {
		name, definition string
		bad              bool
	}{
		{"different CLI", `[]string{"` + k + `"}`, false},
		{"development", `[]string{"Z0vlQMyRQ6f4GlrWdKOdJ5laJlIaaKYxzsfS1AXFLtI="}`, true},
		{"empty", `[]string{}`, true}, {"invalid", `[]string{"broken"}`, true},
		{"multiple", `[]string{"` + k + `","` + k2 + `"}`, false}, {"dynamic", `loadKeys()`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "core/plugin")
			if err := os.MkdirAll(p, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "pubkey.go"), []byte(strings.Replace(releaseKeyRuntime, "var builtinPublicKeysB64 = []string{devPublicKeyB64}", "var builtinPublicKeysB64 = "+tt.definition, 1)), 0644); err != nil {
				t.Fatal(err)
			}
			if err := checkReleaseTargetKeys(root); (err != nil) != tt.bad {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestReleaseTargetKeysRuntimeMismatch(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "core/plugin")
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(releaseKeyRuntime, "for _, s := range builtinPublicKeysB64", "for _, s := range []string{}", 1)
	if err := os.WriteFile(filepath.Join(p, "pubkey.go"), []byte(changed), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseTargetKeys(root); err == nil {
		t.Fatal("unknown runtime reader accepted")
	}
}
