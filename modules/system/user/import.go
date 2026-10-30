package user

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

// 用户导入：支持 .xlsx（pkg/excel，读第一个 sheet）与 CSV（UTF-8，可带 BOM）；按扩展名 + 文件魔数判断类型，不信任 Content-Type。
// 部分成功：逐行独立入库，失败行不影响其它行；失败行（行号 + 原因）随结果返回，前端可下载。

const (
	importMaxBytes     = 2 << 20 // CSV
	importMaxXLSXBytes = 5 << 20 // xlsx 是压缩包，同样行数下比 CSV 大
	importMaxRows      = 5000
)

// importHeader 是模板表头（也是导入的列顺序）。导入时按表头名匹配列，顺序可变，缺列视为空。
var importHeader = []string{"账号", "姓名", "手机", "邮箱", "性别", "部门", "角色", "岗位", "备注"}

var importAliases = map[string]string{
	"账号": "username", "用户名": "username", "username": "username",
	"姓名": "nickname", "昵称": "nickname", "nickname": "nickname",
	"手机": "phone", "手机号": "phone", "phone": "phone",
	"邮箱": "email", "email": "email",
	"性别": "gender", "gender": "gender",
	"部门": "dept", "dept": "dept",
	"角色": "roles", "roles": "roles",
	"岗位": "posts", "posts": "posts",
	"备注": "remark", "remark": "remark",
}

type importRow struct {
	Row                                                                  int // 文件中的行号（表头为 1）
	Username, Nickname, Phone, Email, Gender, Dept, Roles, Posts, Remark string
}

type importFailure struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

// importResult 是 POST /users/import 的响应：{success, failed:[{row,reason}]}。
type importResult struct {
	Success int             `json:"success"`
	Failed  []importFailure `json:"failed"`
}

// rawRec 是文件中的一行原始文本（Line 为文件中的真实行号）。
type rawRec struct {
	Line  int
	Cells []string
}

// parseImportCSV 解析 CSV：去 BOM，按表头名映射列，跳过全空行。
func parseImportCSV(r io.Reader) ([]importRow, error) {
	raw, err := io.ReadAll(io.LimitReader(r, importMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > importMaxBytes {
		return nil, bad("user.import.file_too_large_2mb")
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(raw))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	var recs []rawRec
	for line := 1; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if line == 1 {
				return nil, bad("user.import.parse_csv_failed")
			}
			return nil, bad("user.import.line_invalid", line)
		}
		recs = append(recs, rawRec{Line: line, Cells: rec})
	}
	return buildImportRows(recs)
}

// buildImportRows 把原始行（第一行为表头）按表头名映射成 importRow，跳过全空行。CSV 与 xlsx 共用。
func buildImportRows(recs []rawRec) ([]importRow, error) {
	if len(recs) == 0 {
		return nil, bad("user.import.file_empty")
	}
	col := map[string]int{}
	for i, h := range recs[0].Cells {
		if k, ok := importAliases[strings.ToLower(strings.TrimSpace(h))]; ok {
			if _, dup := col[k]; !dup {
				col[k] = i
			}
		}
	}
	if _, ok := col["username"]; !ok {
		return nil, bad("user.import.missing_username_column")
	}
	var rows []importRow
	for _, rr := range recs[1:] {
		rec, line := rr.Cells, rr.Line
		get := func(k string) string {
			if i, ok := col[k]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		row := importRow{Row: line, Username: get("username"), Nickname: get("nickname"), Phone: get("phone"), Email: get("email"),
			Gender: get("gender"), Dept: get("dept"), Roles: get("roles"), Posts: get("posts"), Remark: get("remark")}
		if row == (importRow{Row: line}) {
			continue
		}
		if len(rows) >= importMaxRows {
			return nil, bad("user.import.too_many_rows", importMaxRows)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// parseImportXLSX 读取 .xlsx 的第一个 sheet：第一行有内容的行是表头，行号取文件中的真实行号。
// 压缩包大小、文件数、行列数、XML 深度等限制见 excel.ReadOptions，防 zip 炸弹与资源耗尽。
func parseImportXLSX(r io.ReaderAt, size int64) ([]importRow, error) {
	opt := excel.ReadOptions{MaxArchiveBytes: importMaxXLSXBytes, MaxFiles: 100, MaxPartBytes: 16 << 20, MaxTotalBytes: 32 << 20,
		MaxRows: importMaxRows * 4, MaxCols: 64, MaxDepth: 32, MaxStrings: 200000}
	rd, err := excel.Open(r, size, opt)
	if err != nil {
		return nil, xlsxError(err)
	}
	var recs []rawRec
	err = rd.ReadRows(func(row int, cells []excel.Cell) error {
		line := make([]string, len(cells))
		for i, c := range cells {
			line[i] = c.String()
		}
		recs = append(recs, rawRec{Line: row, Cells: line})
		return nil
	})
	if err != nil {
		return nil, xlsxError(err)
	}
	return buildImportRows(recs)
}

func xlsxError(err error) error {
	if errors.Is(err, excel.ErrTooLarge) {
		return bad("user.import.xlsx_too_large")
	}
	return bad("user.import.xlsx_parse_failed")
}

// importLookups 是名称 -> ID 的映射（key 为小写名称；角色同时认编码）。
type importLookups struct {
	depts, roles, posts map[string]idgen.ID
}

func splitNames(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '；' || r == '，' || r == '、' || r == '|' })
	var out []string
	for _, x := range f {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func mapNames(raw string, m map[string]idgen.ID, notFoundKey string) ([]string, *rowReason) {
	var ids []string
	for _, n := range splitNames(raw) {
		id, ok := m[strings.ToLower(n)]
		if !ok {
			return nil, &rowReason{key: notFoundKey, args: []any{n}}
		}
		ids = append(ids, id.String())
	}
	return ids, nil
}

func genderCode(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "未知":
		return "0", true
	case "1", "男", "male", "m":
		return "1", true
	case "2", "女", "female", "f":
		return "2", true
	}
	return "", false
}

// rowReason 是行级失败原因：i18n key + 占位符参数。翻译发生在 importUsers（有 ctx 的汇总处），
// 这样报告里的文本跟随请求语言。
type rowReason struct {
	key  string
	args []any
}

type plannedRow struct {
	row    int
	in     userInput
	reason *rowReason // 非空 = 该行校验失败，不入库
}

// planImport 逐行校验（必填、格式、文件内账号唯一、部门/角色/岗位名称映射），不访问数据库。
func planImport(rows []importRow, lk importLookups) []plannedRow {
	seen := map[string]int{}
	out := make([]plannedRow, 0, len(rows))
	for _, r := range rows {
		p := plannedRow{row: r.Row}
		fail := func(key string, args ...any) { p.reason = &rowReason{key: key, args: args} }
		in := userInput{Username: r.Username, Nickname: r.Nickname, Phone: r.Phone, Email: r.Email, Remark: r.Remark, Status: statusOn}
		gender, ok := genderCode(r.Gender)
		switch {
		case r.Username == "":
			fail("user.username_required")
		case !ok:
			fail("user.import.gender_invalid")
		}
		if p.reason == nil {
			in.Gender = gender
			if first, dup := seen[strings.ToLower(r.Username)]; dup {
				fail("user.import.username_duplicate", first)
			}
		}
		if p.reason == nil && r.Dept != "" {
			id, ok := lk.depts[strings.ToLower(r.Dept)]
			if !ok {
				fail("user.import.dept_not_found", r.Dept)
			}
			in.DeptID = id.String()
		}
		if p.reason == nil {
			var reason *rowReason
			if in.RoleIDs, reason = mapNames(r.Roles, lk.roles, "user.import.role_not_found"); reason != nil {
				p.reason = reason
			} else if in.PostIDs, reason = mapNames(r.Posts, lk.posts, "user.import.post_not_found"); reason != nil {
				p.reason = reason
			}
		}
		if p.reason == nil {
			if _, err := (&userInput{Username: in.Username, Nickname: in.Nickname, Phone: in.Phone, Email: in.Email,
				Gender: in.Gender, Status: in.Status, Remark: in.Remark}).check(); err != nil {
				p.reason = rowReasonOf(err)
			}
		}
		if p.reason == nil {
			seen[strings.ToLower(r.Username)] = r.Row
		}
		p.in = in
		out = append(out, p)
	}
	return out
}

// rowReasonOf 把行级校验错误转成 key + 参数：响应错误带 i18n key 时原样透传，否则落到通用原因。
func rowReasonOf(err error) *rowReason {
	var e *response.Error
	if errors.As(err, &e) && e.Key != "" {
		return &rowReason{key: e.Key, args: e.Args}
	}
	return &rowReason{key: "user.import.failed"}
}

// loadLookups 读取当前租户的部门/角色/岗位名称表。
func (s *service) loadLookups(ctx context.Context) (importLookups, error) {
	lk := importLookups{depts: map[string]idgen.ID{}, roles: map[string]idgen.ID{}, posts: map[string]idgen.ID{}}
	tid := int64(tenantOf(ctx))
	db := s.db.WithContext(ctx)
	type nc struct {
		ID   idgen.ID
		Name string
		Code string
	}
	var rs []nc
	if err := db.Table("sys_role").Select("id, name, code").Where("tenant_id = ?", tid).Scan(&rs).Error; err != nil {
		return lk, err
	}
	for _, r := range rs {
		lk.roles[strings.ToLower(r.Code)] = r.ID
		lk.roles[strings.ToLower(r.Name)] = r.ID
	}
	// 部门、岗位属于别的模块；停用或未建表时只是映射不到，对应行会因“不存在”失败。
	var ds []nc
	if err := db.Table("sys_dept").Select("id, name").Where("tenant_id = ?", tid).Scan(&ds).Error; err == nil {
		for _, d := range ds {
			lk.depts[strings.ToLower(d.Name)] = d.ID
		}
	}
	var ps []nc
	if err := db.Table("sys_post").Select("id, name, code").Where("tenant_id = ?", tid).Scan(&ps).Error; err == nil {
		for _, p := range ps {
			lk.posts[strings.ToLower(p.Code)] = p.ID
			lk.posts[strings.ToLower(p.Name)] = p.ID
		}
	}
	return lk, nil
}

// importUsers 校验并逐行创建；每行独立事务，失败行不回滚已成功的行。
//
// 事件：每行走 s.create，成功后与单个创建一样发布 user.created（载荷由 userEvent 构造，只含 id/租户/账号/昵称/状态/部门，
// 不含手机号、邮箱、密码哈希）。批量导入会在短时间内连续发布最多 importMaxRows 个事件；事件总线队列满（默认 1024）时
// 新事件会被丢弃并限频告警——这是总线“宁可丢事件也不拖慢业务”的已知行为，订阅者（如 Webhook）不应把 user.created 当作
// 唯一的全量来源，需要对账时应以用户表为准。
func (s *service) importUsers(ctx context.Context, rows []importRow, log *zap.Logger, me actor) (*importResult, error) {
	lk, err := s.loadLookups(ctx)
	if err != nil {
		return nil, err
	}
	res := &importResult{Failed: []importFailure{}}
	for _, p := range planImport(rows, lk) {
		if p.reason != nil {
			res.Failed = append(res.Failed, importFailure{Row: p.row, Reason: i18n.T(ctx, p.reason.key, p.reason.args...)})
			continue
		}
		if _, err := s.create(ctx, p.in, me); err != nil {
			var e *response.Error
			if errors.As(err, &e) && e.Key != "" {
				// 含「超出套餐用户数」等可预期错误：按当前语言取文案，不做行内重试。
				res.Failed = append(res.Failed, importFailure{Row: p.row, Reason: i18n.T(ctx, e.Key, e.Args...)})
				continue
			}
			if log != nil {
				log.Warn("user import: row failed", zap.Int("row", p.row), zap.Error(err))
			}
			res.Failed = append(res.Failed, importFailure{Row: p.row, Reason: i18n.T(ctx, "user.import.write_failed")})
			continue
		}
		res.Success++
	}
	return res, nil
}

// importTemplate GET /users/import-template：默认下载 .xlsx 模板；?format=csv 返回 CSV 模板（带 BOM，Excel 可直接打开）。
func (h *handler) importTemplate(c *gin.Context) {
	if strings.EqualFold(c.Query("format"), "csv") {
		var buf bytes.Buffer
		buf.WriteString("\xef\xbb\xbf")
		w := csv.NewWriter(&buf)
		_ = w.Write(importHeader)
		_ = w.Write([]string{"demo_user", "演示用户", "13800000000", "demo@example.com", "男", "研发部", "运营;审计", "", ""})
		w.Flush()
		c.Header("Content-Disposition", `attachment; filename="user-import-template.csv"`)
		c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
		return
	}
	var buf bytes.Buffer
	if err := writeImportTemplateXLSX(&buf); err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="user-import-template.xlsx"`)
	c.Data(200, xlsxContentType, buf.Bytes())
}

// importFile POST /users/import（multipart，字段 file，.xlsx 或 .csv）。
func (h *handler) importFile(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, bad("imports.file_required"))
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer f.Close()
	kind, err := sniffImport(fh.Filename, fh.Size, f)
	if err != nil {
		response.Fail(c, err)
		return
	}
	var rows []importRow
	if kind == "xlsx" {
		rows, err = parseImportXLSX(f, fh.Size)
	} else {
		rows, err = parseImportCSV(f)
	}
	if err != nil {
		response.Fail(c, err)
		return
	}
	res, err := h.svc.importUsers(c.Request.Context(), rows, h.log, me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}
