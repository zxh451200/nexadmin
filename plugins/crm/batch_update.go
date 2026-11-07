package crm

// 批量改归属/状态/等级：与批量删除同一套语义——数据范围外的 id 静默跳过，返回实改数。
// 写权限 crm:edit（路由）+ 数据范围（scopedIDs）双重门。每次变更给客户时间线留一条 system 跟进。

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// BatchUpdateInput 是批量改字段的入参：三个字段都可选，只改点名的（nil = 不动）。
type BatchUpdateInput struct {
	IDs    []idgen.ID `json:"ids"`
	Owner  *string    `json:"owner"`
	Status *string    `json:"status"`
	Level  *string    `json:"level"`
}

func (in *BatchUpdateInput) normalize() error {
	if len(in.IDs) == 0 {
		return response.BadRequest("crm.ids_required")
	}
	if in.Owner == nil && in.Status == nil && in.Level == nil {
		return response.BadRequest("crm.batch_nothing")
	}
	if in.Owner != nil {
		owner := strings.TrimSpace(*in.Owner)
		if owner == "" || len([]rune(owner)) > 64 {
			return response.BadRequest("crm.owner_invalid")
		}
		in.Owner = &owner
	}
	if in.Status != nil && *in.Status != StatusActive && *in.Status != StatusInactive {
		return response.BadRequest("crm.status_invalid")
	}
	if in.Level != nil {
		level := truncate(strings.ToUpper(strings.TrimSpace(*in.Level)), levelMaxLen)
		in.Level = &level
	}
	return nil
}

// BatchUpdateCustomers 批量改字段，返回实改数（范围外与无变化的不计）。
func (s *Service) BatchUpdateCustomers(ctx context.Context, sc *datascope.Scope, in BatchUpdateInput) (int64, error) {
	if err := in.normalize(); err != nil {
		return 0, err
	}
	ownable, err := s.scopedIDs(ctx, sc, &Customer{}, in.IDs)
	if err != nil {
		return 0, err
	}
	if len(ownable) == 0 {
		return 0, response.NotFound("crm.not_found")
	}
	var updated int64
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ownable {
			updates := map[string]any{}
			notes := make([]string, 0, 3)
			if in.Owner != nil {
				updates["owner"] = *in.Owner
				notes = append(notes, "归属改为 "+*in.Owner)
			}
			if in.Status != nil {
				updates["status"] = *in.Status
				notes = append(notes, "状态改为 "+*in.Status)
			}
			if in.Level != nil {
				updates["level"] = *in.Level
				notes = append(notes, "等级改为 "+*in.Level)
			}
			res := tx.Model(&Customer{}).Where("id = ?", int64(id)).Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				continue
			}
			updated++
			s.noteSystem(ctx, id, strings.Join(notes, "；"))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}
