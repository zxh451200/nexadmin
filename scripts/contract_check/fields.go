package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// FieldMiss 是“前端 interface 有、后端 DTO/model 没有”的字段。
type FieldMiss struct {
	Interface string `json:"interface"`
	Field     string `json:"field"`
	Optional  bool   `json:"optional"`
	Struct    string `json:"struct"`
	StructAt  string `json:"structAt"`
	Loc
}

// FieldReport 是字段级核对结果；只在命名约定能可靠配对时才比较。
type FieldReport struct {
	Paired      int            `json:"paired"`
	Skipped     int            `json:"skipped"`
	SkipReasons map[string]int `json:"skipReasons"`
	Missing     []FieldMiss    `json:"missing"`
}

const (
	skipNoModule   = "模块目录无法按命名约定配对后端 dto.go/model.go"
	skipRequest    = "请求/表单类 interface（非响应）"
	skipNoStruct   = "找不到唯一的同名后端结构体"
	skipExtends    = "extends 的父类型无法在同文件解析"
	skipNoJSON     = "同名后端结构体没有 json tag（gorm 模型，响应结构在别处）"
	skipIncomplete = "后端结构体含无法解析的嵌入类型"
)

func checkFields(root string, files []*tsFile) FieldReport {
	rep := FieldReport{SkipReasons: map[string]int{}, Missing: []FieldMiss{}}
	structs := loadStructs(root)
	byKey := map[string][]string{} // 配对键 -> 后端目录
	for dir := range structs {
		k := normName(path.Base(dir))
		byKey[k] = append(byKey[k], dir)
	}
	skip := func(why string) { rep.Skipped++; rep.SkipReasons[why]++ }
	for _, f := range files {
		key := moduleKey(f.rel)
		if key == "" {
			continue
		}
		ifaces := parseIfaces(f)
		byName := map[string]tsIface{}
		for _, it := range ifaces {
			byName[it.name] = it
		}
		dirs := byKey[key]
		for _, it := range ifaces {
			switch {
			case len(dirs) != 1:
				skip(skipNoModule)
				continue
			case isRequestType(it.name):
				skip(skipRequest)
				continue
			}
			fields, ok := flatten(it, byName, 0)
			if !ok {
				skip(skipExtends)
				continue
			}
			var hit []*goStruct
			for _, s := range structs[dirs[0]] {
				if ifaceNorm(s.name) == ifaceNorm(it.name) {
					hit = append(hit, s)
				}
			}
			if len(hit) != 1 {
				skip(skipNoStruct)
				continue
			}
			if hit[0].jsonTagged == 0 {
				skip(skipNoJSON)
				continue
			}
			if hit[0].incomplete {
				skip(skipIncomplete)
				continue
			}
			rep.Paired++
			for _, fl := range fields {
				if !hit[0].fields[fl.name] {
					rep.Missing = append(rep.Missing, FieldMiss{Interface: it.name, Field: fl.name, Optional: fl.optional, Struct: hit[0].name,
						StructAt: fmt.Sprintf("%s:%d", hit[0].file, hit[0].line), Loc: Loc{File: f.rel, Line: fl.line}})
				}
			}
		}
	}
	sort.SliceStable(rep.Missing, func(i, j int) bool {
		a, b := rep.Missing[i], rep.Missing[j]
		if a.Optional != b.Optional {
			return !a.Optional // 必填字段缺失更严重，排前面
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return rep
}

// flatten 合并 extends 的同文件父 interface 字段；父类型找不到返回 ok=false。
func flatten(it tsIface, all map[string]tsIface, depth int) ([]tsField, bool) {
	out := append([]tsField{}, it.fields...)
	if depth > 4 {
		return out, false
	}
	for _, e := range it.extends {
		e = strings.TrimSpace(e)
		if i := strings.Index(e, "<"); i >= 0 {
			e = e[:i]
		}
		p, ok := all[e]
		if !ok {
			return nil, false
		}
		pf, ok := flatten(p, all, depth+1)
		if !ok {
			return nil, false
		}
		out = append(out, pf...)
	}
	return out, true
}
