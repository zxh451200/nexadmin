// Command region_import validates explicitly supplied, authorized local data.
// It never downloads data or updates the embedded region package.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type inputNode struct {
	Code       string       `json:"code"`
	Name       string       `json:"name"`
	Pinyin     string       `json:"pinyin,omitempty"`
	ParentCode *string      `json:"parentCode,omitempty"`
	Children   []*inputNode `json:"children,omitempty"`
}

// Keep the output schema compatible with core/region.Node without importing
// that package (and therefore without loading its embedded sample data).
type node struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Pinyin   string  `json:"pinyin"`
	Children []*node `json:"children,omitempty"`
}

type counts struct {
	Total       int `json:"total"`
	Provinces   int `json:"provinces"`
	Prefectures int `json:"prefectures"`
	Counties    int `json:"counties"`
}

type manifest struct {
	SchemaVersion          int      `json:"schemaVersion"`
	SourceURL              string   `json:"sourceURL"`
	Version                string   `json:"version,omitempty"`
	DataDate               string   `json:"dataDate"`
	License                string   `json:"license"`
	AuthorizationStatement string   `json:"authorizationStatement"`
	SourceSHA256           string   `json:"sourceSHA256"`
	OutputSHA256           string   `json:"outputSHA256"`
	Format                 string   `json:"format"`
	Sample                 bool     `json:"sample"`
	Counts                 counts   `json:"counts"`
	DirectCounties         []string `json:"directCounties"`
	ChildlessPrefectures   []string `json:"childlessPrefectures"`
	ValidationScope        string   `json:"validationScope"`
}

type options struct {
	input, output, format, sourceURL, version, date, license, authorization string
	direct, childless                                                       string
	sample                                                                  bool
}

var mainland = map[string]bool{
	"110000": true, "120000": true, "130000": true, "140000": true, "150000": true,
	"210000": true, "220000": true, "230000": true, "310000": true, "320000": true,
	"330000": true, "340000": true, "350000": true, "360000": true, "370000": true,
	"410000": true, "420000": true, "430000": true, "440000": true, "450000": true,
	"460000": true, "500000": true, "510000": true, "520000": true, "530000": true,
	"540000": true, "610000": true, "620000": true, "630000": true, "640000": true,
	"650000": true,
}

func main() {
	var o options
	flag.StringVar(&o.input, "input", "", "必填：已获授权的本地 JSON 数组文件")
	flag.StringVar(&o.output, "out-dir", "", "必填：尚不存在的输出目录（禁止 core/region）")
	flag.StringVar(&o.format, "format", "", "必填：tree 或 flat；flat 根节点 parentCode 必须为 \"\"")
	flag.StringVar(&o.sourceURL, "source-url", "", "必填：来源 HTTP(S) URL，仅记录，不访问")
	flag.StringVar(&o.version, "version", "", "可选：来源版本标识")
	flag.StringVar(&o.date, "data-date", "", "必填：数据版本日期 YYYY-MM-DD")
	flag.StringVar(&o.license, "license", "", "必填：许可证或授权协议标识")
	flag.StringVar(&o.authorization, "authorization", "", "必填：提供者对数据使用和转换权利的明确授权声明")
	flag.StringVar(&o.direct, "direct-counties", "", "逗号分隔：明确确认直接隶属省的县级代码（不补中间层）")
	flag.StringVar(&o.childless, "childless-prefectures", "", "逗号分隔：明确确认无县级下辖的地级市代码")
	flag.BoolVar(&o.sample, "sample", false, "显式样例模式：允许少于大陆31省及省级叶节点")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "region_import: 不接受位置参数")
		os.Exit(1)
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "region_import:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	for _, field := range []struct{ name, value string }{
		{"input", o.input}, {"out-dir", o.output}, {"source-url", o.sourceURL},
		{"data-date", o.date}, {"license", o.license}, {"authorization", o.authorization},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("必须提供 -%s", field.name)
		}
	}
	if o.format != "tree" && o.format != "flat" {
		return errors.New("-format 必须为 tree 或 flat")
	}
	u, err := url.Parse(o.sourceURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return errors.New("-source-url 必须是无用户凭据的 HTTP(S) URL")
	}
	if _, err := time.Parse("2006-01-02", o.date); err != nil {
		return fmt.Errorf("-data-date: %w", err)
	}
	direct, err := codeSet(o.direct, 3)
	if err != nil {
		return fmt.Errorf("-direct-counties: %w", err)
	}
	childless, err := codeSet(o.childless, 2)
	if err != nil {
		return fmt.Errorf("-childless-prefectures: %w", err)
	}
	out, err := outputPath(o.output)
	if err != nil {
		return err
	}
	f, err := os.Open(o.input)
	if err != nil {
		return err
	}
	const maxBytes = 64 << 20
	raw, readErr := io.ReadAll(io.LimitReader(f, maxBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(raw) > maxBytes {
		return errors.New("输入超过64MiB限制")
	}
	var records []*inputNode
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&records); err != nil {
		return fmt.Errorf("JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("JSON 数组后存在额外内容")
	}
	if len(records) == 0 {
		return errors.New("输入数组不能为空")
	}
	roots, totals, err := convert(records, o, direct, childless)
	if err != nil {
		return err
	}
	tree, err := json.MarshalIndent(roots, "", "  ")
	if err != nil {
		return err
	}
	tree = append(tree, '\n')
	sourceSum, outputSum := sha256.Sum256(raw), sha256.Sum256(tree)
	m := manifest{
		SchemaVersion: 1, SourceURL: o.sourceURL, Version: o.version,
		DataDate: o.date, License: o.license, AuthorizationStatement: o.authorization,
		SourceSHA256: hex.EncodeToString(sourceSum[:]), OutputSHA256: hex.EncodeToString(outputSum[:]),
		Format: o.format, Sample: o.sample, Counts: totals,
		DirectCounties: sortedKeys(direct), ChildlessPrefectures: sortedKeys(childless),
		ValidationScope: "结构、代码形态及省级覆盖校验；来源和授权为提供者声明，未在线核实，不证明各省市县数据完整或现行有效；未生成拼音或合成行政层级",
	}
	meta, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// A new directory and exclusive files prevent overwriting any existing data.
	if err := os.Mkdir(out, 0755); err != nil {
		return fmt.Errorf("创建全新输出目录: %w", err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{"regions.json", tree}, {"manifest.json", append(meta, '\n')}} {
		if err := writeExclusive(filepath.Join(out, file.name), file.data); err != nil {
			return fmt.Errorf("输出失败（可能存在部分产物，请人工检查 %s）: %w", out, err)
		}
	}
	fmt.Printf("离线校验及转换完成：%s；省=%d 地级=%d 县级=%d 总数=%d sample=%t\n",
		out, totals.Provinces, totals.Prefectures, totals.Counties, totals.Total, o.sample)
	return nil
}

func level(code string) int {
	if len(code) != 6 {
		return 0
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0
		}
	}
	if !mainland[code[:2]+"0000"] {
		return 0
	}
	if code[2:] == "0000" {
		return 1
	}
	if code[2:4] == "00" {
		return 0
	}
	if code[4:] == "00" {
		return 2
	}
	return 3
}

func municipality(code string) bool {
	return code == "110000" || code == "120000" || code == "310000" || code == "500000"
}

func codeSet(text string, wantLevel int) (map[string]bool, error) {
	result := map[string]bool{}
	if text == "" {
		return result, nil
	}
	for _, item := range strings.Split(text, ",") {
		code := strings.TrimSpace(item)
		if level(code) != wantLevel || result[code] {
			return nil, fmt.Errorf("代码 %q 层级不符或重复", code)
		}
		result[code] = true
	}
	return result, nil
}

func convert(records []*inputNode, o options, direct, childless map[string]bool) ([]*node, counts, error) {
	nodes := map[string]*node{}
	parents := map[string]string{}
	var totals counts
	var add func(*inputNode, string, int) error
	add = func(in *inputNode, parent string, depth int) error {
		if in == nil || depth > 3 {
			return errors.New("存在 null 节点或树深度超过三级")
		}
		l := level(in.Code)
		if l == 0 || strings.TrimSpace(in.Name) == "" || in.Name != strings.TrimSpace(in.Name) {
			return fmt.Errorf("无效的大陆6位代码或名称: %q / %q", in.Code, in.Name)
		}
		if _, exists := nodes[in.Code]; exists {
			return fmt.Errorf("重复代码: %s", in.Code)
		}
		if o.format == "flat" {
			if in.ParentCode == nil || len(in.Children) != 0 {
				return fmt.Errorf("flat 节点 %s 必须声明 parentCode 且不能包含子节点", in.Code)
			}
			parent = *in.ParentCode
		} else if in.ParentCode != nil && *in.ParentCode != parent {
			return fmt.Errorf("tree 节点 %s 的 parentCode 与树位置冲突", in.Code)
		}
		nodes[in.Code] = &node{Code: in.Code, Name: in.Name, Pinyin: in.Pinyin}
		parents[in.Code] = parent
		totals.Total++
		switch l {
		case 1:
			totals.Provinces++
		case 2:
			totals.Prefectures++
		case 3:
			totals.Counties++
		}
		for _, child := range in.Children {
			if err := add(child, in.Code, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, record := range records {
		if err := add(record, "", 1); err != nil {
			return nil, totals, err
		}
	}
	roots := make([]*node, 0)
	usedDirect := map[string]bool{}
	for _, code := range sortedKeys(nodes) {
		n := nodes[code]
		parent := parents[code]
		if level(code) == 1 {
			if parent != "" {
				return nil, totals, fmt.Errorf("省级 %s 不得有父节点", code)
			}
			roots = append(roots, n)
			continue
		}
		p, exists := nodes[parent]
		if !exists || parent[:2] != code[:2] {
			return nil, totals, fmt.Errorf("%s 父节点 %q 缺失或跨省", code, parent)
		}
		valid := false
		if level(parent) == 1 {
			if municipality(parent) {
				// Municipal districts/counties remain direct children. Reject
				// synthetic "市辖区/县" prefecture codes such as 110100.
				valid = level(code) == 3
			} else if level(code) == 2 {
				// xx90 is a grouping for province-administered counties,
				// not an actual prefecture to synthesize.
				valid = code[2:4] != "90"
			} else if direct[code] {
				valid = true
				usedDirect[code] = true
			}
		} else if level(parent) == 2 && level(code) == 3 {
			valid = parent[:4] == code[:4]
		}
		if !valid {
			return nil, totals, fmt.Errorf("非法父子层级 %s -> %s；不得伪造中间层，省直辖县需显式声明", parent, code)
		}
		p.Children = append(p.Children, n)
	}
	for _, code := range sortedKeys(nodes) {
		n := nodes[code]
		switch level(code) {
		case 1:
			if !o.sample && len(n.Children) == 0 {
				return nil, totals, fmt.Errorf("正式模式省级 %s 缺少下级", code)
			}
		case 2:
			if len(n.Children) == 0 && !childless[code] {
				return nil, totals, fmt.Errorf("地级 %s 无县级下辖，需 -childless-prefectures 明确确认", code)
			}
			if len(n.Children) != 0 && childless[code] {
				return nil, totals, fmt.Errorf("声明的无县地级市 %s 实际有子节点", code)
			}
		}
	}
	for _, code := range sortedKeys(direct) {
		if !usedDirect[code] {
			return nil, totals, fmt.Errorf("省直辖县声明 %s 未用于相应直接隶属关系", code)
		}
	}
	for _, code := range sortedKeys(childless) {
		if nodes[code] == nil {
			return nil, totals, fmt.Errorf("无县地级市声明 %s 不存在", code)
		}
	}
	if !o.sample && len(roots) != len(mainland) {
		return nil, totals, fmt.Errorf("正式模式要求大陆31省覆盖，实际 %d；样例请显式 -sample", len(roots))
	}
	return roots, totals, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func outputPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Resolve the existing parent before checking the protected path so that
	// symlink aliases cannot direct output into core/region.
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("输出父目录必须已存在: %w", err)
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	parts := strings.Split(filepath.ToSlash(abs), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "core" && parts[i+1] == "region" {
			return "", errors.New("禁止向 core/region 及其子目录输出")
		}
	}
	if _, err := os.Lstat(abs); !os.IsNotExist(err) {
		return "", errors.New("输出目录必须尚不存在")
	}
	return abs, nil
}

func writeExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return closeErr
}
