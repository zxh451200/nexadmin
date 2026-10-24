package formbuilder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 收集与导出的资源上限。
const (
	scanBatch       = 500
	MaxFilterScan   = 5000   // 按字段筛选时最多扫描最新的多少条（字段值在 JSON 里，不走索引）
	MaxSyncExport   = 5000   // 同步导出的行数上限；更多请用导出中心
	MaxAsyncExport  = 100000 // 导出中心任务的行数上限
	maxFilterValLen = 200
)

var errExportTooLarge = bad("导出的行数超过上限，请缩小筛选范围")

// SubRow 是提交列表/详情里的一行。
type SubRow struct {
	ID        idgen.ID       `json:"id"`
	Version   int            `json:"version"`
	UserID    idgen.ID       `json:"userId"`
	Username  string         `json:"username"`
	CreatedAt time.Time      `json:"createdAt"`
	Data      map[string]any `json:"data"`
}

func toRow(sub *Submission) SubRow {
	data := map[string]any{}
	_ = json.Unmarshal([]byte(sub.Data), &data)
	return SubRow{ID: sub.ID, Version: sub.Version, UserID: sub.UserID, Username: sub.Username, CreatedAt: sub.CreatedAt, Data: data}
}

// SubMeta 是提交列表页需要的表单信息：最新已发布版本的定义（用来决定列），以及全部版本号（用来筛选）。
type SubMeta struct {
	ID         idgen.ID    `json:"id"`
	Name       string      `json:"name"`
	Version    int         `json:"version"`
	Versions   []int       `json:"versions"`
	Definition *Definition `json:"definition"`
}

// SubMeta 返回提交列表的元信息。
func (s *Service) SubMeta(ctx context.Context, id idgen.ID) (*SubMeta, error) {
	f, err := s.store.GetForm(ctx, id)
	if err != nil {
		return nil, err
	}
	vs, err := s.store.ListVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	m := &SubMeta{ID: f.ID, Name: f.Name, Versions: []int{}, Definition: &Definition{Fields: []Field{}}}
	for _, v := range vs {
		m.Versions = append(m.Versions, v.Version)
		if v.Version == f.PublishedVersion {
			if d, err := decodeDefinition([]byte(v.Definition)); err == nil {
				m.Definition, m.Version = d, v.Version
			}
		}
	}
	return m, nil
}

// SubPage 是提交列表的一页。Truncated = 按字段筛选时扫描达到上限，结果可能不完整。
type SubPage struct {
	List      []SubRow `json:"list"`
	Total     int64    `json:"total"`
	Page      int      `json:"page"`
	PageSize  int      `json:"pageSize"`
	Truncated bool     `json:"truncated"`
}

// ListSubmissions 分页列出提交。没有字段筛选时走数据库分页；有字段筛选时在内存里匹配最新的 MaxFilterScan 条。
func (s *Service) ListSubmissions(ctx context.Context, q SubQuery) (*SubPage, error) {
	if _, err := s.store.GetForm(ctx, q.FormID); err != nil {
		return nil, err
	}
	if err := checkFilter(q); err != nil {
		return nil, err
	}
	page := &SubPage{List: []SubRow{}, Page: q.Page, PageSize: q.PageSize}
	if q.Field == "" {
		total, err := s.store.CountSubmissions(ctx, q)
		if err != nil {
			return nil, err
		}
		rows, err := s.store.PageSubmissions(ctx, q)
		if err != nil {
			return nil, err
		}
		page.Total = total
		for i := range rows {
			page.List = append(page.List, toRow(&rows[i]))
		}
		return page, nil
	}
	var matched []SubRow
	scanned, err := s.iterate(ctx, q, MaxFilterScan, func(sub *Submission) bool {
		matched = append(matched, toRow(sub))
		return true
	})
	if err != nil {
		return nil, err
	}
	page.Truncated = scanned >= MaxFilterScan
	page.Total = int64(len(matched))
	start := (q.Page - 1) * q.PageSize
	if start < len(matched) {
		page.List = matched[start:min(start+q.PageSize, len(matched))]
	}
	return page, nil
}

func checkFilter(q SubQuery) error {
	if q.Field != "" && !keyRe.MatchString(q.Field) {
		return bad("筛选字段不合法")
	}
	if len([]rune(q.Value)) > maxFilterValLen || len([]rune(q.Username)) > 64 {
		return bad("筛选条件过长")
	}
	return nil
}

// iterate 按 id 倒序分批扫描符合条件的提交（含字段筛选），最多检查 limit 条；返回检查过的条数。fn 返回 false 提前结束。
func (s *Service) iterate(ctx context.Context, q SubQuery, limit int, fn func(*Submission) bool) (int, error) {
	var before idgen.ID
	scanned := 0
	for scanned < limit {
		batch, err := s.store.ScanSubmissions(ctx, q, before, min(scanBatch, limit-scanned))
		if err != nil {
			return scanned, err
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			scanned++
			before = batch[i].ID
			if q.Field != "" && !matchField(batch[i].Data, q.Field, q.Value) {
				continue
			}
			if !fn(&batch[i]) {
				return scanned, nil
			}
		}
		if len(batch) < scanBatch {
			break
		}
	}
	return scanned, nil
}

// matchField 判断提交数据里字段 key 的值是否匹配 want：文本做不区分大小写的包含匹配，其余类型精确匹配，
// 多选/文件在任一元素匹配时命中。want 为空时匹配"该字段有值"。
func matchField(data, key, want string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(data), &m) != nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	return matchValue(v, strings.ToLower(want))
}

func matchValue(v any, want string) bool {
	switch x := v.(type) {
	case string:
		return want == "" || strings.Contains(strings.ToLower(x), want)
	case float64:
		return want == "" || strconv.FormatFloat(x, 'f', -1, 64) == want
	case bool:
		return want == "" || strconv.FormatBool(x) == want
	case []any:
		if want == "" {
			return len(x) > 0
		}
		for _, e := range x {
			if mm, ok := e.(map[string]any); ok {
				if n, _ := mm["name"].(string); strings.Contains(strings.ToLower(n), want) {
					return true
				}
				continue
			}
			if matchValue(e, want) {
				return true
			}
		}
	}
	return false
}

// SubDetail 是单条提交详情：数据 + 它所用版本的定义（改版后历史数据仍按当时的定义展示）。
type SubDetail struct {
	SubRow
	Definition *Definition `json:"definition"`
}

// GetSubmission 返回详情。
func (s *Service) GetSubmission(ctx context.Context, formID, id idgen.ID) (*SubDetail, error) {
	sub, err := s.store.GetSubmission(ctx, formID, id)
	if err != nil {
		return nil, err
	}
	v, err := s.store.GetVersion(ctx, formID, sub.Version)
	if err != nil {
		return nil, err
	}
	def, err := decodeDefinition([]byte(v.Definition))
	if err != nil {
		return nil, err
	}
	return &SubDetail{SubRow: toRow(sub), Definition: def}, nil
}

// DeleteSubmission 删除一条提交。
func (s *Service) DeleteSubmission(ctx context.Context, formID, id idgen.ID) error {
	return s.store.DeleteSubmission(ctx, formID, id)
}

// Roles / Users 给设计器的可见范围选择器提供候选项。
func (s *Service) Roles(ctx context.Context) ([]RoleOption, error) {
	list, err := s.dir.Roles(ctx)
	if list == nil {
		list = []RoleOption{}
	}
	return list, err
}

func (s *Service) Users(ctx context.Context, keyword string) ([]UserOption, error) {
	list, err := s.dir.Users(ctx, keyword, 20)
	if list == nil {
		list = []UserOption{}
	}
	return list, err
}

// ---- 导出 ----

type exportCol struct{ Key, Title string }

// exportColumns 取所有版本字段的并集：按首次出现的顺序排列，标题取最新出现的版本，所以改版删字段后历史数据仍能导出。
func exportColumns(defs map[int]*Definition, versions []int) []exportCol {
	var cols []exportCol
	idx := map[string]int{}
	for _, v := range versions {
		for _, f := range defs[v].Fields {
			if !f.hasValue() {
				continue
			}
			if i, ok := idx[f.Key]; ok {
				cols[i].Title = f.Title
				continue
			}
			idx[f.Key] = len(cols)
			cols = append(cols, exportCol{f.Key, f.Title})
		}
	}
	return cols
}

var exportHead = map[string][4]string{
	"zh-CN": {"提交ID", "提交人", "提交时间", "版本"},
	"en-US": {"Submission ID", "Submitter", "Submitted at", "Version"},
}

// WriteExport 把符合条件的提交写成 xlsx（最新的在前），最多 maxRows 行，超过则返回 errExportTooLarge 且不应使用已写内容。
// 文本以 = + - @ 开头时由 excel 包加前缀，防 CSV/公式注入。
func (s *Service) WriteExport(ctx context.Context, w io.Writer, q SubQuery, maxRows int, lang string) (int, error) {
	if err := checkFilter(q); err != nil {
		return 0, err
	}
	if _, err := s.store.GetForm(ctx, q.FormID); err != nil {
		return 0, err
	}
	vs, err := s.store.ListVersions(ctx, q.FormID)
	if err != nil {
		return 0, err
	}
	defs := make(map[int]*Definition, len(vs))
	order := make([]int, 0, len(vs))
	for _, v := range vs {
		d, err := decodeDefinition([]byte(v.Definition))
		if err != nil {
			return 0, err
		}
		defs[v.Version] = d
		order = append(order, v.Version)
	}
	cols := exportColumns(defs, order)
	head, ok := exportHead[lang]
	if !ok {
		head = exportHead["zh-CN"]
	}
	header := append([]string{}, head[:]...)
	for _, c := range cols {
		header = append(header, c.Title)
	}
	x := excel.NewWriter(w, excel.Options{FormulaGuard: excel.GuardPrefix})
	sh, err := x.NewSheet("Submissions", excel.SheetOptions{Header: header})
	if err != nil {
		return 0, err
	}
	rows := 0
	var werr error
	_, err = s.iterate(ctx, q, 1<<30, func(sub *Submission) bool {
		if rows >= maxRows {
			werr = errExportTooLarge
			return false
		}
		def := defs[sub.Version]
		var data map[string]any
		_ = json.Unmarshal([]byte(sub.Data), &data)
		vals := []any{sub.ID.String(), sub.Username, sub.CreatedAt.UTC(), sub.Version}
		for _, c := range cols {
			vals = append(vals, cellValue(def, c.Key, data[c.Key]))
		}
		if werr = sh.WriteRow(vals...); werr != nil {
			return false
		}
		rows++
		return ctx.Err() == nil
	})
	if err == nil {
		err = werr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return rows, err
	}
	return rows, x.Close()
}

// cellValue 把一个提交值转成单元格值：选项换成名称，多值用顿号连接，文件取文件名。
func cellValue(def *Definition, key string, v any) any {
	if v == nil || def == nil {
		return nil
	}
	var f *Field
	for i := range def.Fields {
		if def.Fields[i].Key == key {
			f = &def.Fields[i]
			break
		}
	}
	label := func(val any) string {
		s, _ := val.(string)
		if f != nil {
			for _, o := range f.Options {
				if o.Value == s {
					return o.Label
				}
			}
		}
		return s
	}
	switch x := v.(type) {
	case string:
		return label(x)
	case float64, bool:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				n, _ := m["name"].(string)
				parts = append(parts, n)
			} else {
				parts = append(parts, label(e))
			}
		}
		return strings.Join(parts, "、")
	}
	return fmt.Sprint(v)
}
