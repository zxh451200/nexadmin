package recycle

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"nexadmin/core/response"
	"nexadmin/modules/internal/recbin"
	"nexadmin/pkg/idgen"
)

// Service 是回收站的业务层。
type Service struct{ db *gorm.DB }

// Item 与前端 RecycleItem 一致。
type Item struct {
	ID        idgen.ID  `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	DeletedBy string    `json:"deletedBy"`
	DeletedAt time.Time `json:"deletedAt"`
}

// List 按删除时间倒序。
func (s *Service) List(ctx context.Context, typ, keyword string, page, size int) ([]Item, int64, error) {
	tx := s.db.WithContext(ctx).Model(&recbin.Entry{})
	if typ != "" {
		tx = tx.Where("type = ?", typ)
	}
	if keyword != "" {
		r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		tx = tx.Where("name LIKE ?", "%"+r.Replace(keyword)+"%")
	}
	tx = tx.Session(&gorm.Session{})
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []recbin.Entry
	if err := tx.Order("deleted_at DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]Item, len(rows))
	for i, e := range rows {
		out[i] = Item{ID: e.ID, Type: e.Type, Name: e.Name, DeletedBy: e.DeletedBy, DeletedAt: e.DeletedAt}
	}
	return out, total, nil
}

func (s *Service) entry(ctx context.Context, id idgen.ID) (*recbin.Entry, string, error) {
	var e recbin.Entry
	if err := s.db.WithContext(ctx).First(&e, "id = ?", int64(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", response.NotFound("error.not_found")
		}
		return nil, "", err
	}
	table, ok := recbin.Tables[e.Type]
	if !ok {
		return nil, "", response.BadRequest("recycle.type_unsupported")
	}
	return &e, table, nil
}

// errSourceGone 表示回收记录指向的源行已不存在（被清掉或已不处于软删除状态）。
func errSourceGone() error { return response.NotFound("recycle.source_gone") }

// Restore 还原：清掉源表的 deleted_at，并移出回收站。
// 原生 SQL 不经过租户 scope，所以显式带 tenant_id（取回收记录自己的租户）。
//
// 源行已不存在（RowsAffected==0）时：返回 404，并且仍然清掉这条回收记录。
// 取舍：只返回 404 而保留记录，用户会反复点到同一条永远失败的记录，列表无法自愈；
// 而返回成功会谎报“已还原”。源数据没了，记录本身没有任何还原 / 彻底删除的价值，所以两者都做：
// 在事务里删除记录并提交，事务外再返回 404（不能在事务回调里返回错误，否则清理会被一并回滚）。
// 条件带 deleted_at IS NOT NULL：不依赖各引擎对“值未变化”的 RowsAffected 口径差异（MySQL 默认不计）。
func (s *Service) Restore(ctx context.Context, id idgen.ID) error {
	e, table, err := s.entry(ctx, id)
	if err != nil {
		return err
	}
	gone := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("UPDATE "+table+" SET deleted_at = NULL WHERE id = ? AND tenant_id = ? AND deleted_at IS NOT NULL", int64(e.RefID), int64(e.TenantID))
		if res.Error != nil {
			// 多半是唯一约束冲突（同名用户已被重新创建）
			return response.Conflict("recycle.restore_conflict").Wrap(res.Error)
		}
		gone = res.RowsAffected == 0
		return tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}

// Purge 彻底删除：真正删掉源表的行（只删仍处于软删除状态的），并移出回收站。
// 源行已不存在（RowsAffected==0）时与 Restore 同样处理：清掉回收记录并提交，再返回 404（取舍见 Restore）。
func (s *Service) Purge(ctx context.Context, id idgen.ID) error {
	e, table, err := s.entry(ctx, id)
	if err != nil {
		return err
	}
	gone := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ref, tid := int64(e.RefID), int64(e.TenantID)
		if e.Type == recbin.TypeNotice { // 公告的发布对象与已读记录一并清理
			for _, t := range []string{"sys_notice_target", "sys_notice_read"} {
				if err := tx.Exec("DELETE FROM "+t+" WHERE notice_id = ?", ref).Error; err != nil {
					return err
				}
			}
		}
		res := tx.Exec("DELETE FROM "+table+" WHERE id = ? AND tenant_id = ? AND deleted_at IS NOT NULL", ref, tid)
		if res.Error != nil {
			return res.Error
		}
		gone = res.RowsAffected == 0
		return tx.Delete(&recbin.Entry{}, "id = ?", int64(e.ID)).Error
	})
	if err != nil {
		return err
	}
	if gone {
		return errSourceGone()
	}
	return nil
}
