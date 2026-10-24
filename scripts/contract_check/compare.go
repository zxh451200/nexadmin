package main

import (
	"fmt"
	"sort"
	"strings"
)

// Issue 种类；数值越小越严重。
const (
	kindMissing = "missing" // 后端没有这条路径
	kindMethod  = "method"  // 路径存在，但方法对不上
	kindWeak    = "weak"    // 仅靠后端 :id / ? 占位吞掉前端静态段才对上，疑似拼写错误
)

var kindSeverity = map[string]int{kindMissing: 1, kindMethod: 2, kindWeak: 3}

// Issue 是 A / B 类里的一条不一致（同一 METHOD+路径的多处调用合并）。
type Issue struct {
	Method   string   `json:"method"`
	Path     string   `json:"path"`
	Module   string   `json:"module"`
	Kind     string   `json:"kind"`
	Severity int      `json:"severity"`
	Locs     []Loc    `json:"locs"`
	Note     string   `json:"note"`
	Suggest  []string `json:"suggest,omitempty"`
	Called   bool     `json:"calledByFrontend,omitempty"` // B 类：前端代码里是否也真的调用了它
}

// Summary 是各类数量。
type Summary struct {
	BackendRoutes  int `json:"backendRoutes"`
	BackendDynamic int `json:"backendDynamic"`
	Perms          int `json:"perms"`
	FrontCalls     int `json:"frontCalls"`     // 唯一 METHOD+路径
	FrontCallSites int `json:"frontCallSites"` // 调用点
	MockRoutes     int `json:"mockRoutes"`
	A              int `json:"a"`
	B              int `json:"b"`
	C              int `json:"c"`
	Unresolved     int `json:"unresolved"`
}

// Report 是完整核对结果。
type Report struct {
	Summary    Summary      `json:"summary"`
	A          []Issue      `json:"a"`
	B          []Issue      `json:"b"`
	C          []Route      `json:"c"`
	Fields     FieldReport  `json:"fields"`
	Unresolved []Unresolved `json:"unresolved"`
	Perms      []string     `json:"perms"`
}

// Check 扫描仓库并生成报告。
func Check(root string) (*Report, error) {
	files, err := loadFront(root)
	if err != nil {
		return nil, fmt.Errorf("读取前端源码失败: %w", err)
	}
	be := scanBackend(root)
	calls, fu := frontCalls(files)
	mocks, mu := frontMocks(files)
	return build(be, calls, mocks, append(fu, mu...), checkFields(root, files)), nil
}

func build(be *BackendScan, calls, mocks []Call, fu []Unresolved, fields FieldReport) *Report {
	rep := &Report{Fields: fields, Perms: sortedKeys(be.Perms)}
	rep.Unresolved = append(append([]Unresolved{}, be.Unres...), fu...)
	used := make([]bool, len(be.Routes))
	callKeys := map[string]bool{}
	for _, c := range calls {
		callKeys[c.Method+" "+pathKey(c.Path)] = true
	}
	rep.A = classify(calls, be.Routes, used, nil)
	rep.B = classify(mocks, be.Routes, make([]bool, len(be.Routes)), callKeys)
	for i, r := range be.Routes {
		if r.Dynamic {
			rep.Summary.BackendDynamic++
		} else if !used[i] && strings.HasPrefix(r.Path, apiPrefix+"/") {
			rep.C = append(rep.C, r)
		}
	}
	sort.SliceStable(rep.C, func(i, j int) bool {
		a, b := rep.C[i], rep.C[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Path < b.Path
	})
	rep.Summary.BackendRoutes = len(be.Routes)
	rep.Summary.Perms = len(be.Perms)
	rep.Summary.FrontCalls = len(callKeys)
	rep.Summary.FrontCallSites = len(calls)
	rep.Summary.MockRoutes = len(uniqueKeys(mocks))
	rep.Summary.A, rep.Summary.B, rep.Summary.C = len(rep.A), len(rep.B), len(rep.C)
	rep.Summary.Unresolved = len(rep.Unresolved)
	return rep
}

func uniqueKeys(cs []Call) map[string]bool {
	m := map[string]bool{}
	for _, c := range cs {
		m[c.Method+" "+pathKey(c.Path)] = true
	}
	return m
}
