package crm

// 疑似重复客户巡检：按手机号 / 证件号 / 信用代码分组，同值多客户 = 疑似重复。
// 保存时的查重是「入口闸」，这里是「存量巡检」——历史脏数据（导入、锚点旁路、手工录入）
// 平时不可见，有了这份清单才能引导去合并。分组按三把身份证据逐一扫，命中组数封顶防放大。

import (
	"context"

	"gorm.io/gorm"

	"nexadmin/core/datascope"
)

// maxDuplicateGroups 是返回的疑似重复组数上限：每组还要回查一次成员，放开等于放大查询数。
const maxDuplicateGroups = 50

// DuplicateGroup 是一组疑似重复：同一身份证据下挂着多个客户。
type DuplicateGroup struct {
	Key       string     `json:"key"`   // phone | id_no | tax_no
	Value     string     `json:"value"` // 命中的值（证件号原样，前端按权限脱敏展示）
	Rows      []Customer `json:"customers"`
}

// ListDuplicates 找出疑似重复的分组。数据范围生效：受限账号只看到自己范围内的客户
// 参与分组——公海与别人的客户不该出现在他的巡检清单里。
func (s *Service) ListDuplicates(ctx context.Context, sc *datascope.Scope) ([]DuplicateGroup, error) {
	out := []DuplicateGroup{}
	for _, spec := range []struct{ key, col string }{{"phone", "phone"}, {"id_no", "id_no"}, {"tax_no", "tax_no"}} {
		// 先取「同值多客户」的键，再回查每组的成员行；两步都带数据范围
		groupDB := func() *gorm.DB {
			db := s.db.WithContext(ctx).Model(&Customer{}).
				Select(spec.col+" AS k, COUNT(*) AS n").
				Where(spec.col+" <> ''").
				Group(spec.col).Having("COUNT(*) > 1")
			if cond, args, limited := sc.UsernameCond("owner"); limited {
				db = db.Where(cond, args...)
			}
			return db
		}
		var keys []struct {
			K string
			N int64
		}
		if err := groupDB().Scan(&keys).Error; err != nil {
			return nil, err
		}
		for _, g := range keys {
			var rows []Customer
			members := s.db.WithContext(ctx).Where(spec.col+" = ?", g.K).Order("id")
			if cond, args, limited := sc.UsernameCond("owner"); limited {
				members = members.Where(cond, args...)
			}
			if err := members.Find(&rows).Error; err != nil {
				return nil, err
			}
			if len(rows) < 2 {
				continue // 数据范围裁过之后可能只剩一条，不再算重复
			}
			out = append(out, DuplicateGroup{Key: spec.key, Value: g.K, Rows: rows})
			if len(out) >= maxDuplicateGroups {
				return out, nil
			}
		}
	}
	return out, nil
}
