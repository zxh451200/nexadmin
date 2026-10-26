package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// 批量签发：同一客户、多个插件一次签出。每个插件独立序列号与台账记录（可单独吊销），
// 打包成 zip 下载（<plugin>.lic + LICENSES.txt 清单）。授权载荷格式与单个签发完全一致。

const maxBundlePlugins = 50

type bundleReq struct {
	Plugins  []string `json:"plugins"`
	Customer string   `json:"customer"`
	Expires  string   `json:"expires"`
	Domains  string   `json:"domains"`
	Note     string   `json:"note"`
}

// issueBundleCore 校验并循环签出（不落盘、不打包）；任一插件失败整体失败。
func (s *server) issueBundleCore(in bundleReq, now time.Time) ([][]byte, []Record, error) {
	seen := map[string]bool{}
	var ids []string
	for _, p := range in.Plugins {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if seen[p] {
			return nil, nil, fmt.Errorf("插件 %q 重复", p)
		}
		seen[p] = true
		ids = append(ids, p)
	}
	if len(ids) == 0 {
		return nil, nil, errors.New("至少填一个插件 id")
	}
	if len(ids) > maxBundlePlugins {
		return nil, nil, fmt.Errorf("一次最多签发 %d 个插件", maxBundlePlugins)
	}
	var raws [][]byte
	var recs []Record
	for _, id := range ids {
		raw, rec, err := s.issueLicense(issueReq{
			Plugin: id, Customer: in.Customer, Expires: in.Expires, Domains: in.Domains, Note: in.Note,
		}, now)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", id, err)
		}
		raws = append(raws, raw)
		recs = append(recs, rec)
	}
	return raws, recs, nil
}

func (s *server) issueBundle(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var in bundleReq
	if r.Header.Get("X-Requested-With") != "licensor" || json.NewDecoder(r.Body).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式错误"})
		return
	}
	raws, recs, err := s.issueBundleCore(in, s.now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 先全部落台账再回包：任何一条写不进去都整体失败，避免"授权已发出却没有记录"。
	for _, rec := range recs {
		if err := s.st.Add(rec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "写入签发记录失败，未返回授权包: " + rec.Plugin})
			return
		}
	}
	zb, err := bundleZip(in.Customer, recs, raws)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "打包 zip 失败"})
		return
	}
	log.Printf("已批量签发 %d 个插件 customer=%q expires=%s kid=%s", len(recs), in.Customer, recs[0].ExpiresAt, s.kid)
	setHeaders(w)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="licenses-%s-%s.zip"`, sanitizeFilename(in.Customer), time.Now().Format("20060102")))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(zb)
}

// bundleZip 生成 <plugin>.lic + LICENSES.txt（客户/到期/序列号清单）的内存 zip，条目按插件 id 排序保证可重现。
func bundleZip(customer string, recs []Record, raws [][]byte) ([]byte, error) {
	sort.Slice(recs, func(i, j int) bool { return recs[i].Plugin < recs[j].Plugin })
	var buf strings.Builder
	buf.WriteString("NexAdmin 授权包\r\n")
	buf.WriteString("客户: " + customer + "\r\n")
	buf.WriteString("签发时间: " + time.Now().Format("2006-01-02 15:04") + "\r\n")
	buf.WriteString("导入方式: 后台「插件管理」逐个导入对应 .lic（或粘贴文件内容）\r\n\r\n")
	for _, rec := range recs {
		buf.WriteString(fmt.Sprintf("- %s: 有效期至 %s，序列号 %s\r\n", rec.Plugin, rec.ExpiresAt[:10], rec.Serial))
	}
	var out strings.Builder
	zw := zip.NewWriter(&out)
	for i, rec := range recs {
		w, err := zw.Create(rec.Plugin + ".lic")
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(raws[i]); err != nil {
			return nil, err
		}
	}
	manifest, err := zw.Create("LICENSES.txt")
	if err != nil {
		return nil, err
	}
	if _, err := manifest.Write([]byte(buf.String())); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// sanitizeFilename 把客户名转成安全文件名片段（非安全字符折叠为 -）。
func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "bundle"
	}
	name := b.String()
	if len(name) > 40 {
		name = name[:40]
	}
	return name
}
