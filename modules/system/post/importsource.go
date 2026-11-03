package post

// importsource.go 把「岗位」接进通用导入中心：组织架构落地时岗位总是成批出现的，
// 一条条在表格里攒没有意义。契约在 pkg/importspec，框架模块实现它不会反向依赖导入中心插件。

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

// ImportSourcePosts 是岗位导入源的标识，也是 i18n 里 imports.source.<name>.* 的那段键。
const ImportSourcePosts = "sys_posts"

// 编译期钉住接口：导入中心靠 importspec.Provider 在挂载时自动收集来源，
// 方法签名一旦漂了就不会有任何运行期报错，岗位导入会静默从列表里消失。
var _ importspec.Provider = module{}

// ImportSources 实现 importspec.Provider：导入中心挂载时自动收集，随模块启停出现。
func (module) ImportSources(db *gorm.DB) []importspec.Source {
	svc := NewService(db)
	return []importspec.Source{{
		Name:     ImportSourcePosts,
		Title:    "岗位导入",
		TitleKey: "imports.source.sys_posts.title",
		Columns: []importspec.Column{
			{Key: "code", Label: "岗位编码", LabelKey: "imports.source.sys_posts.code", Required: true, Example: "seo"},
			{Key: "name", Label: "岗位名称", LabelKey: "imports.source.sys_posts.name", Required: true, Example: "总裁"},
			{Key: "sort", Label: "排序", LabelKey: "imports.source.sys_posts.sort", Example: "1"},
			{Key: "status", Label: "状态(1启用 0停用)", LabelKey: "imports.source.sys_posts.status", Example: "1"},
			{Key: "remark", Label: "备注", LabelKey: "imports.source.sys_posts.remark"},
		},
		// 没有 dept 列：部门只有名称、没有稳定的人类可读编码（sys_dept 的唯一键是 tenant_id+name？
		// 见 model.go，实际按 parent_id 组树），按名称匹配会遇到同名部门而悄悄挂错父级。
		// 宁可导入完在页面上指定归属，也不要造一个会静默错挂的列。
		Handler: func(ctx context.Context, _ int, row map[string]string) error {
			return importPost(ctx, svc, row)
		},
	}}
}

// importPost 按岗位编码认重：sys_post 有 uk_sys_post_tenant_code 唯一索引，编码就是租户内的稳定主键，
// 同一份文件重复导入只会更新同名岗位，不会堆出重复行。
func importPost(ctx context.Context, svc *Service, row map[string]string) error {
	code := strings.TrimSpace(row["code"])
	name := strings.TrimSpace(row["name"])
	if code == "" || name == "" {
		return errors.New(i18n.T(ctx, "imports.source.sys_posts.name_or_code_required"))
	}
	sortVal := 0
	if s := strings.TrimSpace(row["sort"]); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return errors.New(i18n.T(ctx, "imports.source.sys_posts.sort_invalid"))
		}
		sortVal = n
	}
	in := Input{
		Name: name, Code: code, Sort: sortVal,
		Status: strings.TrimSpace(row["status"]), Remark: strings.TrimSpace(row["remark"]),
	}
	var existing Post
	err := svc.db.WithContext(ctx).Where("code = ?", code).Take(&existing).Error
	switch {
	case err == nil:
		_, uerr := svc.Update(ctx, idgen.ID(existing.ID), in)
		return rowErrText(ctx, uerr)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return err
	}
	_, cerr := svc.Create(ctx, in)
	return rowErrText(ctx, cerr)
}

// rowErrText 把业务错误的 i18n key 翻成当前语言的句子——导入报告是给人看的，不能是裸 key。
func rowErrText(ctx context.Context, err error) error {
	var re *response.Error
	if errors.As(err, &re) && re.Msg == "" && re.Key != "" {
		return errors.New(i18n.T(ctx, re.Key, re.Args...))
	}
	return err
}
