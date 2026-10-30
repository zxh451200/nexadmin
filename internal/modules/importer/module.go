// Package importer 是「导入中心」的示例业务模块：框架出厂的系统表没有附带任何导入源，
// 导入中心在母版里因此是一个空壳（来源下拉为空、按钮置灰）。这里按 AGENTS.md 的约定
// 在用户区（internal/）演示如何用自己的表接入：plugins/imports 只出解析、模板、台账与
// 错误报告，逐行业务逻辑留在这里。
//
// 当前提供「数据字典」一个源：按 字典编码 找到字典类型，按 值 覆盖或新增字典项。
// 之所以选字典：它是平台级表（不分租户），而导入中心的大文件异步执行跑在
// context.Background() 上，拿不到请求级租户；租户表要先解决这一点再接。
package importer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core"
	"nexadmin/modules/system/dict"
	"nexadmin/plugins/imports"
)

func init() { core.RegisterBuiltin(module{}) }

type module struct{}

func (module) Name() string { return "importer" }

// Register 本模块只提供导入源、不注册路由：实现 SourceProvider 后导入中心会在挂载时
// 自动收集，不需要在这里多写一次注册。
func (module) Register(_ *gin.RouterGroup, _ *core.Deps) {}

// ImportSources 让导入中心发现「数据字典」这个源。
func (module) ImportSources(db *gorm.DB) []imports.Source { return []imports.Source{dictSource(db)} }

// dictSource 把一处导入源的定义收起来，便于测试直接断言模板列与回调。
func dictSource(db *gorm.DB) imports.Source {
	return imports.Source{
		Name:  "sys-dict-data",
		Title: "数据字典",
		Columns: []imports.Column{
			{Key: "typeCode", Label: "字典编码", Required: true, Example: "gender"},
			{Key: "label", Label: "标签", Required: true, Example: "男"},
			{Key: "value", Label: "值", Required: true, Example: "1"},
			{Key: "sort", Label: "排序", Example: "10"},
			{Key: "status", Label: "状态", Example: "启用"},
			{Key: "remark", Label: "备注", Example: ""},
		},
		Handler: dictDataHandler(db),
	}
}

// dictDataHandler 逐行写入 sys_dict_data：同类型 + 同值视为同一条，覆盖标签/排序/状态/备注。
func dictDataHandler(db *gorm.DB) func(context.Context, int, map[string]string) error {
	return func(ctx context.Context, _ int, row map[string]string) error {
		code := strings.TrimSpace(row["typeCode"])
		value := strings.TrimSpace(row["value"])
		label := strings.TrimSpace(row["label"])
		if code == "" || value == "" || label == "" {
			return errors.New("字典编码、标签、值都不能为空")
		}
		status, err := normalizeStatus(row["status"])
		if err != nil {
			return err
		}
		sort, err := normalizeSort(row["sort"])
		if err != nil {
			return err
		}
		remark := strings.TrimSpace(row["remark"])

		var typ dict.Type
		err = db.WithContext(ctx).Where("code = ?", code).First(&typ).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("字典类型 %q 不存在，先到「字典管理」建类型", code)
		}
		if err != nil {
			return err
		}

		var cur dict.Data
		err = db.WithContext(ctx).Where("type_id = ? AND dict_value = ?", typ.ID, value).First(&cur).Error
		switch {
		case err == nil:
			return db.WithContext(ctx).Model(&dict.Data{}).Where("id = ?", cur.ID).Updates(map[string]any{
				"dict_label": label, "sort": sort, "status": status, "remark": remark,
			}).Error
		case errors.Is(err, gorm.ErrRecordNotFound):
			if sort == 0 {
				sort = nextSort(ctx, db, typ.ID)
			}
			return db.WithContext(ctx).Create(&dict.Data{
				TypeID: typ.ID, Label: label, Value: value, Sort: sort, Status: status, Remark: remark,
			}).Error
		default:
			return err
		}
	}
}

// nextSort 给没填排序的新行续在末尾，避免整批全是 0 堆在一起。
func nextSort(ctx context.Context, db *gorm.DB, typeID interface{}) int {
	var max int
	if err := db.WithContext(ctx).Model(&dict.Data{}).Where("type_id = ?", typeID).
		Select("COALESCE(MAX(sort), 0)").Scan(&max).Error; err != nil {
		return 0
	}
	return max + 10
}

// normalizeStatus 允许中英文与 1/0，未填写默认启用。
func normalizeStatus(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1", "启用", "是", "enabled", "enable", "true", "on", "yes":
		return "1", nil
	case "0", "停用", "否", "disabled", "disable", "false", "off", "no":
		return "0", nil
	default:
		return "", fmt.Errorf("状态 %q 无法识别，可填 启用/停用 或 1/0", raw)
	}
}

// normalizeSort 未填写返回 0（由调用方续号）。
func normalizeSort(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("排序 %q 不是非负整数", raw)
	}
	return n, nil
}
