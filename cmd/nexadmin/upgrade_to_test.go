package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestUpgradeToChecksPackageVersion(t *testing.T) {
	root := upNewProject(t)
	pkg := upPkg(t, upFwManifest("1.1.0"), map[string]string{"core/a.go": "new a\n"})
	var out bytes.Buffer
	if err := upgrade(root, upOptions{From: pkg, To: "1.1.0", Manifest: frameworkManifest, AllowDirty: true}, &out, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := upgrade(root, upOptions{From: pkg, To: "2.0.0", Manifest: frameworkManifest, AllowDirty: true}, &out, time.Now()); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("expected version mismatch, got %v", err)
	}
}
