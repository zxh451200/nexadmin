package crm

// import.go 把客户主数据接进通用导入中心：客户是最常见的"先用 Excel 攒着"的数据，
// 出厂就该能导。导入中心启动时扫描已启用模块，发现本模块实现了 ImportSources
// 即自动挂上这个来源，不需要在任何地方再写一遍接线。

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/importspec"
)

// SourceCustomers 是客户导入源的标识（台账里按它归类）。
const SourceCustomers = "crm_customers"

// 导入表里没人写 "person"：这两张别名表把人话映射到类型码，识别不了的整行失败，不猜。
var customerTypeAliases = map[string]string{
	"person": TypePerson, "individual": TypePerson, "个人": TypePerson, "自然人": TypePerson,
	"company": TypeCompany, "corp": TypeCompany, "corporate": TypeCompany, "enterprise": TypeCompany,
	"企业": TypeCompany, "公司": TypeCompany, "单位": TypeCompany,
}

var idTypeAliases = map[string]string{
	"id_card": IdTypeIDCard, "身份证": IdTypeIDCard,
	"passport": IdTypePassport, "护照": IdTypePassport,
	"officer_card": IdTypeOfficer, "军官证": IdTypeOfficer,
	"hmt_permit": IdTypeHMTPermit, "港澳台通行证": IdTypeHMTPermit,
	"other": IdTypeOther, "其他": IdTypeOther,
}

// parseTypeAlias 空值走 def（类型缺省个人、证件类型缺省未登记）。
func parseTypeAlias(raw string, def string, aliases map[string]string, errKey string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return def, nil
	}
	if hit, ok := aliases[v]; ok {
		return hit, nil
	}
	return "", response.BadRequest(errKey)
}

// ImportSources 实现 importspec.Provider。
func (m *module) ImportSources(db *gorm.DB) []importspec.Source {
	svc := NewService(db, zapLog{zap.NewNop()})
	return []importspec.Source{{
		Name:     SourceCustomers,
		Title:    "客户导入",
		TitleKey: "imports.source.crm_customers.title",
		Columns: []importspec.Column{
			{Key: "name", Label: "客户名称", LabelKey: "imports.source.crm_customers.name", Required: true, Example: "张三"},
			{Key: "type", Label: "客户类型", LabelKey: "imports.source.crm_customers.type", Example: "个人"},
			{Key: "phone", Label: "手机号", LabelKey: "imports.source.crm_customers.phone", Example: "13800138000"},
			{Key: "email", Label: "邮箱", LabelKey: "imports.source.crm_customers.email", Example: "zhangsan@example.com"},
			{Key: "level", Label: "客户等级", LabelKey: "imports.source.crm_customers.level", Example: "A"},
			{Key: "owner", Label: "负责人", LabelKey: "imports.source.crm_customers.owner", Example: "王五"},
			{Key: "country", Label: "国家/地区", LabelKey: "imports.source.crm_customers.country", Example: "CN"},
			{Key: "nationality", Label: "国籍", LabelKey: "imports.source.crm_customers.nationality", Example: "CN"},
			{Key: "id_type", Label: "证件类型", LabelKey: "imports.source.crm_customers.id_type", Example: "身份证"},
			{Key: "id_no", Label: "证件号", LabelKey: "imports.source.crm_customers.id_no", Example: "33010619900101001X"},
			{Key: "tax_no", Label: "统一社会信用代码", LabelKey: "imports.source.crm_customers.tax_no", Example: "91330100MA2AB1CD3X"},
			{Key: "industry", Label: "所属行业", LabelKey: "imports.source.crm_customers.industry", Example: "住宿业"},
			{Key: "address", Label: "企业注册地址", LabelKey: "imports.source.crm_customers.address"},
			{Key: "remark", Label: "备注", LabelKey: "imports.source.crm_customers.remark"},
		},
		Handler: func(ctx context.Context, _ int, row map[string]string) error {
			return importCustomer(ctx, svc, row)
		},
	}}
}

// existingByImportKey 认行：手机号 → 证件号 → 统一社会信用代码，第一个非空且命中的键胜出。
// 企业行通常没有手机号，只按手机号认会越导越多重复客户。
func existingByImportKey(ctx context.Context, svc *Service, in CustomerInput) (*Customer, error) {
	for _, q := range []struct{ cond, val string }{
		{"phone = ?", in.Phone}, {"id_no = ?", in.IdNo}, {"tax_no = ?", in.TaxNo},
	} {
		val := strings.TrimSpace(q.val)
		if val == "" {
			continue
		}
		var c Customer
		err := svc.db.WithContext(ctx).Where(q.cond, val).Take(&c).Error
		if err == nil {
			return &c, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

// importCustomer 按认行键命中则更新、未命中则新建，重复导入同一份文件不会翻倍。
func importCustomer(ctx context.Context, svc *Service, row map[string]string) error {
	typ, err := parseTypeAlias(row["type"], TypePerson, customerTypeAliases, "crm.type_invalid")
	if err != nil {
		return rowError(ctx, err)
	}
	idType, err := parseTypeAlias(row["id_type"], "", idTypeAliases, "crm.id_type_invalid")
	if err != nil {
		return rowError(ctx, err)
	}
	in := CustomerInput{
		Name: row["name"], Type: typ, Phone: row["phone"], Email: row["email"],
		Level: row["level"], Owner: row["owner"], Remark: row["remark"],
		Country: row["country"], Nationality: row["nationality"],
		IdType: idType, IdNo: row["id_no"],
		TaxNo: row["tax_no"], Industry: row["industry"], Address: row["address"],
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New(i18n.T(ctx, "imports.required_empty"))
	}
	existing, err := existingByImportKey(ctx, svc, in)
	if err != nil {
		return err
	}
	if existing != nil {
		in.Status = existing.Status
		// 导入在后台任务里跑，不经过 HTTP 身份链：sc 传 nil 表示不裁剪
		//（能进导入入口 already 由 crm:edit 权限把守，见 datascope 的 nil 约定）。
		_, uerr := svc.UpdateCustomer(ctx, nil, existing.ID, in)
		return rowError(ctx, uerr)
	}
	_, err = svc.CreateCustomer(ctx, in)
	return rowError(ctx, err)
}

// rowError 把业务错误的 i18n key 翻成当前语言的句子——导入报告是给人看的，
// 不能把 crm.name_required 这种 key 直接写进去。
func rowError(ctx context.Context, err error) error {
	var re *response.Error
	if errors.As(err, &re) && re.Msg == "" && re.Key != "" {
		return errors.New(i18n.T(ctx, re.Key, re.Args...))
	}
	return err
}
