package user

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"nexadmin/core"
	"nexadmin/pkg/excel"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

var (
	magicZip = []byte("PK\x03\x04")
	magicOLE = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1} // 旧版 .xls
)

// sniffImport 按扩展名 + 文件头魔数判断导入文件类型（"xlsx" / "csv"）。不看 Content-Type，两者必须一致。
// 大小按本次请求生效的 importLimits 判（lim 来自 resolveImportLimits），报错里带出的是那个值，不是内置默认。
func sniffImport(lim importLimits, filename string, size int64, f io.ReaderAt) (string, error) {
	head := make([]byte, 512)
	n, _ := f.ReadAt(head, 0)
	head = head[:n]
	isZip := bytes.HasPrefix(head, magicZip)
	isOLE := bytes.HasPrefix(head, magicOLE)
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		if !isZip {
			return "", bad("user.import.not_xlsx")
		}
		if size > lim.xlsx {
			return "", bad("user.import.file_too_large", core.MBText(lim.xlsx))
		}
		return "xlsx", nil
	case ".csv":
		if isZip || isOLE || bytes.IndexByte(head, 0) >= 0 {
			return "", bad("user.import.csv_encoding_invalid")
		}
		if size > lim.csv {
			return "", bad("user.import.file_too_large", core.MBText(lim.csv))
		}
		return "csv", nil
	case ".xls":
		return "", bad("user.import.legacy_xls_unsupported")
	}
	return "", bad("user.import.format_only_xlsx_csv")
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

// writeUserRow 写一行导出数据（异步导出任务用，列顺序对应 exportHeader）。
func writeUserRow(s *excel.Sheet, u *userDTO) error {
	status := "停用"
	if u.Status == statusOn {
		status = "启用"
	}
	return s.WriteRow(u.Username, u.Nickname, u.Phone, u.Email, genderText(u.Gender), u.DeptName,
		strings.Join(u.RoleNames, ";"), u.Remark, status, u.CreatedAt)
}
