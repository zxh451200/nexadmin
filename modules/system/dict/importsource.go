package dict

// importsource.go 把「字典数据」接进通用导入中心：字典项是后台最常见的“先在表格里攒一批”
// 的数据。契约在 pkg/importspec，所以框架模块实现它不会反向依赖导入中心插件。

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/i18n"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
	"nexadmin/pkg/importspec"
)

// ImportSourceData 是字典数据的导入源标识。
const ImportSourceData = "dict_data"

// ImportSources 实现 importspec.Provider，由导入中心在挂载时自动收集。
func (module) ImportSources(db *gorm.DB) []importspec.Source {
	svc := NewService(db)
	return []importspec.Source{{
		Name:     ImportSourceData,
		Title:    "字典数据导入",
		TitleKey: "imports.source.dict_data.title",
		Columns: []importspec.Column{
			{Key: "type_code", Label: "字典类型编码", LabelKey: "imports.source.dict_data.type_code", Required: true, Example: "sys_common_status"},
			{Key: "label", Label: "标签", LabelKey: "imports.source.dict_data.label", Required: true, Example: "成功"},
			{Key: "value", Label: "值", LabelKey: "imports.source.dict_data.value", Required: true, Example: "1"},
			{Key: "tag", Label: "标签样式", LabelKey: "imports.source.dict_data.tag", Example: "success"},
			{Key: "sort", Label: "排序", LabelKey: "imports.source.dict_data.sort", Example: "10"},
			{Key: "status", Label: "状态(1启用 0停用)", LabelKey: "imports.source.dict_data.status", Example: "1"},
			{Key: "remark", Label: "备注", LabelKey: "imports.source.dict_data.remark"},
		},
		Handler: func(ctx context.Context, _ int, row map[string]string) error {
			return importData(ctx, svc, row)
		},
	}}
}

// importData 按「类型 + 值」认重：命中就更新标签/样式/排序/状态/备注，未命中新建，
// 所以同一份文件重复导入不会造出重复字典项。
func importData(ctx context.Context, svc *Service, row map[string]string) error {
	code := strings.TrimSpace(row["type_code"])
	var t Type
	if err := svc.db.WithContext(ctx).Where("code = ?", code).Take(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(i18n.T(ctx, "dict.type_not_found"))
		}
		return err
	}
	sortVal := 0
	if s := strings.TrimSpace(row["sort"]); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return errors.New(i18n.T(ctx, "imports.source.dict_data.sort_invalid"))
		}
		sortVal = n
	}
	in := DataInput{
		TypeID: t.ID, Label: strings.TrimSpace(row["label"]), Value: strings.TrimSpace(row["value"]),
		Tag: strings.TrimSpace(row["tag"]), Sort: sortVal,
		Status: strings.TrimSpace(row["status"]), Remark: strings.TrimSpace(row["remark"]),
	}
	var existing Data
	err := svc.db.WithContext(ctx).
		Where("type_id = ? AND dict_value = ?", int64(t.ID), in.Value).Take(&existing).Error
	switch {
	case err == nil:
		_, uerr := svc.UpdateData(ctx, idgen.ID(existing.ID), in)
		return rowErrText(ctx, uerr)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return err
	}
	_, cerr := svc.CreateData(ctx, in)
	return rowErrText(ctx, cerr)
}

// rowErrText 把业务错误的 i18n key 翻成当前语言的句子：导入报告是给人看的。
func rowErrText(ctx context.Context, err error) error {
	var re *response.Error
	if errors.As(err, &re) && re.Msg == "" && re.Key != "" {
		return errors.New(i18n.T(ctx, re.Key, re.Args...))
	}
	return err
}
