package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plugin sign --ledger：授权写文件的同时向 issued.json 追加一条与 licensor store.Record 同格式的记录。
func TestSignWritesLedger(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "private.pem")
	var keyOut strings.Builder
	if err := cmdKeygen([]string{"--out", keyFile}, &keyOut); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, "issued.json")
	licFile := filepath.Join(dir, "cron.lic")

	args := []string{
		"--key", keyFile, "--plugin", "cron", "--customer", "测试客户",
		"--expires", "2027-12-31", "--out", licFile, "--ledger", ledger,
	}
	var out strings.Builder
	if err := cmdSign(args, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"授权文件已写入", "台账已追加"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
	raw, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var recs []ledgerRecord
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("ledger not a JSON array: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.ID == "" || rec.ID != rec.Serial || rec.Plugin != "cron" || rec.Customer != "测试客户" || rec.KID == "" {
		t.Errorf("bad record: %+v", rec)
	}
	// 二次签发追加而非覆盖。
	var out2 strings.Builder
	if err := cmdSign([]string{"--key", keyFile, "--plugin", "audit", "--customer", "测试客户",
		"--expires", "2027-12-31", "--ledger", ledger}, &out2); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(ledger)
	if err := json.Unmarshal(raw, &recs); err != nil || len(recs) != 2 {
		t.Fatalf("ledger should have 2 records after second sign: %v", err)
	}
	if !strings.Contains(strings.Join([]string{recs[0].Plugin, recs[1].Plugin}, ","), "audit") {
		t.Error("second record plugin missing")
	}
	// 签出的授权文件存在且非空。
	if fi, err := os.Stat(licFile); err != nil || fi.Size() == 0 {
		t.Fatalf("license file missing or empty: %v", err)
	}
}
