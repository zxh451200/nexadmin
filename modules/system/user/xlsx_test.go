package user

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestImportTemplateXLSXParses(t *testing.T) {
	var buf bytes.Buffer
	if err := writeImportTemplateXLSX(&buf); err != nil {
		t.Fatal(err)
	}
	rows, err := parseImportXLSX(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if r := rows[0]; r.Row != 2 || r.Username != "demo_user" || r.Phone != "13800000000" || r.Gender != "男" || r.Roles != "运营;审计" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestExportXLSXRoundTripToImport(t *testing.T) {
	ts := time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC)
	list := []*userDTO{
		{Username: "alice", Nickname: "爱丽丝😀", Phone: "+8613800000001", Email: "a@x.com", Gender: "2", DeptName: "研发部",
			RoleNames: []string{"运营", "审计"}, Status: statusOn, Remark: "=1+1", CreatedAt: ts},
		{Username: "bob", Status: statusOff, CreatedAt: ts},
	}
	var buf bytes.Buffer
	if err := writeUsersXLSX(&buf, list); err != nil {
		t.Fatal(err)
	}
	// 导出的文件可以直接再导入（表头同名）；敏感字段不会出现在任何部件里
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		if s := strings.ToLower(b.String()); strings.Contains(s, "password") || strings.Contains(s, "密码") || strings.Contains(s, "$2a$") {
			t.Fatalf("sensitive field leaked in %s", f.Name)
		}
	}
	rows, err := parseImportXLSX(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	a := rows[0]
	if a.Username != "alice" || a.Nickname != "爱丽丝😀" || a.Phone != "+8613800000001" || a.Gender != "女" ||
		a.Dept != "研发部" || a.Roles != "运营;审计" || a.Remark != "=1+1" || a.Row != 2 || rows[1].Row != 3 {
		t.Fatalf("unexpected: %+v", a)
	}
}

func TestSniffImport(t *testing.T) {
	zipHead := []byte("PK\x03\x04rest")
	cases := []struct {
		name string
		body []byte
		want string // 空 = 期望报错
	}{
		{"a.xlsx", zipHead, "xlsx"},
		{"A.XLSX", zipHead, "xlsx"},
		{"a.csv", []byte("账号,姓名\r\n"), "csv"},
		{"a.xlsx", []byte("账号,姓名\r\n"), ""},                                   // 扩展名是 xlsx，内容却是 CSV
		{"a.csv", zipHead, ""},                                                // 扩展名是 csv，内容却是 zip
		{"a.csv", []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, ""}, // 旧版 xls 伪装
		{"a.csv", []byte("a\x00b"), ""},                                       // UTF-16 / 二进制
		{"a.xls", []byte{0xD0, 0xCF, 0x11, 0xE0}, ""},
		{"a.txt", []byte("x"), ""},
		{"a", []byte("x"), ""},
	}
	for _, c := range cases {
		got, err := sniffImport(c.name, int64(len(c.body)), bytes.NewReader(c.body))
		if (err == nil) != (c.want != "") || got != c.want {
			t.Errorf("%s %q: got %q err=%v want %q", c.name, c.body, got, err, c.want)
		}
	}
}

func TestParseImportXLSXRejectsBadFiles(t *testing.T) {
	// 损坏文件：不 panic，返回错误
	for _, b := range [][]byte{[]byte("PK\x03\x04garbage"), []byte("PK\x03\x04"), nil} {
		if _, err := parseImportXLSX(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("expected error for %q", b)
		}
	}
	// zip 炸弹：压缩后很小，解压后超出单部件上限
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xl/workbook.xml")
	_, _ = w.Write(bytes.Repeat([]byte(" "), 40<<20))
	_ = zw.Close()
	if buf.Len() > importMaxXLSXBytes {
		t.Fatalf("bomb too big to be a bomb: %d", buf.Len())
	}
	if _, err := parseImportXLSX(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err == nil {
		t.Error("zip bomb accepted")
	}
}
