package ctl

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDumpGzipWritesCompressedHashedPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := filepath.Join(t.TempDir(), "backup")
	path := filepath.Join(dir, "nexadmin-mysql-v1-20261017-030405.sql.gz")
	// 假的 mysqldump：输出固定 SQL，并检查密码只在环境变量里
	spec := ToolSpec{Name: "sh", Args: []string{"-c", `echo "-- dump"; test "$MYSQL_PWD" = "pw" && echo "SELECT 1;"`}, Env: []string{"MYSQL_PWD=pw"}, Secret: "pw"}
	res, err := DumpGzip(context.Background(), spec, path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", fi.Mode().Perm())
	}
	if err := VerifySHA256(path, res.SHA256); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadSidecar(path); got != res.SHA256 {
		t.Errorf("sidecar %q != %q", got, res.SHA256)
	}
	f, _ := os.Open(path)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(gz)
	if !strings.Contains(string(b), "SELECT 1;") {
		t.Errorf("unexpected content %q", b)
	}
	if fi, _ := os.Stat(path); res.Size != fi.Size() {
		t.Errorf("size %d != file size %d", res.Size, fi.Size())
	}
}

func TestDumpGzipFailureLeavesNothingAndHidesPassword(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.sql.gz")
	spec := ToolSpec{Name: "sh", Args: []string{"-c", `echo "access denied using pw=topsecret" >&2; exit 2`}, Secret: "topsecret"}
	_, err := DumpGzip(context.Background(), spec, path)
	if err == nil || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("want error without password, got %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("failed dump must leave no files, found %d", len(ents))
	}
}
