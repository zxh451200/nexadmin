package user

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexadmin/core/response"
	"nexadmin/pkg/excel"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

var (
	magicZip = []byte("PK\x03\x04")
	magicOLE = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1} // 旧版 .xls
)

// sniffImport 按扩展名 + 文件头魔数判断导入文件类型（"xlsx" / "csv"）。不看 Content-Type，两者必须一致。
func sniffImport(filename string, size int64, f io.ReaderAt) (string, error) {
	head := make([]byte, 512)
	n, _ := f.ReadAt(head, 0)
	head = head[:n]
	isZip := bytes.HasPrefix(head, magicZip)
	isOLE := bytes.HasPrefix(head, magicOLE)
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		if !isZip {
			return "", bad("文件内容不是有效的 .xlsx，请使用导入模板")
		}
		if size > importMaxXLSXBytes {
			return "", bad("文件不能超过 5MB")
		}
		return "xlsx", nil
	case ".csv":
		if isZip || isOLE || bytes.IndexByte(head, 0) >= 0 {
			return "", bad("文件内容与扩展名不符：.csv 需为 UTF-8 文本")
		}
		if size > importMaxBytes {
			return "", bad("文件不能超过 2MB")
		}
		return "csv", nil
	case ".xls":
		return "", bad("不支持旧版 .xls，请另存为 .xlsx 后再导入")
	}
	return "", bad("仅支持 .xlsx 或 .csv 文件")
}

// writeImportTemplateXLSX 写导入模板：第一个 sheet 是表头 + 示例行（性别列带下拉），第二个 sheet 是填写说明。
// 说明放在独立 sheet 里，因为导入只读第一个 sheet，说明行不会被当成数据。
func writeImportTemplateXLSX(w io.Writer) error {
	x := excel.NewWriter(w)
	s, err := x.NewSheet("用户", excel.SheetOptions{
		Header:      importHeader,
		Validations: []excel.Validation{{Col: 4, List: []string{"男", "女", "未知"}, FirstRow: 2, LastRow: 5001}},
	})
	if err != nil {
		return err
	}
	if err := s.WriteRow("demo_user", "演示用户", "13800000000", "demo@example.com", "男", "研发部", "运营;审计", "", ""); err != nil {
		return err
	}
	help, err := x.NewSheet("填写说明", excel.SheetOptions{Header: []string{"列", "说明"}})
	if err != nil {
		return err
	}
	for _, r := range [][2]string{
		{"账号", "必填；2-64 位字母、数字或 _ . @ -；不能与已有账号或文件内其它行重复"},
		{"姓名", "选填"},
		{"手机 / 邮箱", "选填；格式需正确。手机号请按文本填写，避免丢失前导 0 或 +"},
		{"性别", "男 / 女 / 未知（可下拉选择）"},
		{"部门", "按部门名称填写，必须已存在"},
		{"角色 / 岗位", "按名称（角色也可填编码）填写，多个用分号 ; 分隔，必须已存在"},
		{"其它", "第一个 sheet 的第一行是表头，不要改列名；示例行 demo_user 请删除或改成真实数据；一次最多导入 5000 行"},
	} {
		if err := help.WriteRow(r[0], r[1]); err != nil {
			return err
		}
	}
	return x.Close()
}

var exportHeader = []string{"账号", "姓名", "手机", "邮箱", "性别", "部门", "角色", "备注", "状态", "创建时间"}

func genderText(g string) string {
	switch g {
	case "1":
		return "男"
	case "2":
		return "女"
	}
	return "未知"
}

// writeUserRow 写一行导出数据（同步与异步导出共用，列顺序对应 exportHeader）。
func writeUserRow(s *excel.Sheet, u *userDTO) error {
	status := "停用"
	if u.Status == statusOn {
		status = "启用"
	}
	return s.WriteRow(u.Username, u.Nickname, u.Phone, u.Email, genderText(u.Gender), u.DeptName,
		strings.Join(u.RoleNames, ";"), u.Remark, status, u.CreatedAt)
}

// writeUsersXLSX 把用户列表流式写成 .xlsx。列是显式白名单：只有 exportHeader 里的字段，
// 密码哈希等敏感字段既不在 userDTO 里，也不会被写出。
func writeUsersXLSX(w io.Writer, list []*userDTO) error {
	x := excel.NewWriter(w)
	s, err := x.NewSheet("用户", excel.SheetOptions{Header: exportHeader})
	if err != nil {
		return err
	}
	for _, u := range list {
		if err := writeUserRow(s, u); err != nil {
			return err
		}
	}
	return x.Close()
}

// exportXLSX 沿用 service.export（同样的筛选条件、数据范围和行数上限），仅改变输出格式。
func (h *handler) exportXLSX(c *gin.Context) {
	list, err := h.svc.export(c.Request.Context(), queryOf(c), me(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="users-%s.xlsx"`, time.Now().Format("20060102")))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(200)
	if err := writeUsersXLSX(c.Writer, list); err != nil && h.log != nil {
		// 响应头已发出，无法再改状态码；记录日志，客户端会拿到不完整的文件
		h.log.Warn("user export xlsx failed", zap.Error(err))
	}
}
