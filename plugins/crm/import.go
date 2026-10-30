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

// ImportSources 实现 importspec.Provider。
func (m *module) ImportSources(db *gorm.DB) []importspec.Source {
	svc := NewService(db, zapLog{zap.NewNop()})
	return []importspec.Source{{
		Name:     SourceCustomers,
		Title:    "客户导入",
		TitleKey: "imports.source.crm_customers.title",
		Columns: []importspec.Column{
			{Key: "name", Label: "客户名称", LabelKey: "imports.source.crm_customers.name", Required: true, Example: "张三"},
			{Key: "phone", Label: "手机号", LabelKey: "imports.source.crm_customers.phone", Example: "13800138000"},
			{Key: "email", Label: "邮箱", LabelKey: "imports.source.crm_customers.email", Example: "zhangsan@example.com"},
			{Key: "level", Label: "客户等级", LabelKey: "imports.source.crm_customers.level", Example: "A"},
			{Key: "owner", Label: "负责人", LabelKey: "imports.source.crm_customers.owner", Example: "王五"},
			{Key: "remark", Label: "备注", LabelKey: "imports.source.crm_customers.remark"},
		},
		Handler: func(ctx context.Context, _ int, row map[string]string) error {
			return importCustomer(ctx, svc, row)
		},
	}}
}

// importCustomer 按手机号认人：命中则更新，未命中则新建，重复导入同一份文件不会翻倍。
func importCustomer(ctx context.Context, svc *Service, row map[string]string) error {
	in := CustomerInput{
		Name: row["name"], Phone: row["phone"], Email: row["email"],
		Level: row["level"], Owner: row["owner"], Remark: row["remark"],
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New(i18n.T(ctx, "imports.required_empty"))
	}
	if phone := strings.TrimSpace(in.Phone); phone != "" {
		var existing Customer
		err := svc.db.WithContext(ctx).Where("phone = ?", phone).Take(&existing).Error
		switch {
		case err == nil:
			in.Status = existing.Status
			_, uerr := svc.UpdateCustomer(ctx, existing.ID, in)
			return rowError(ctx, uerr)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
	}
	_, err := svc.CreateCustomer(ctx, in)
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
