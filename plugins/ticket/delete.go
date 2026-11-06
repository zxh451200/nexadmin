package ticket

// 删除工单：垃圾单/测试单的清理性出口。取向：
// - 不设状态门槛：删除是管理员级高危操作（独立权限点 ticket:delete + 操作日志），能删就是真删；
//   想保留服务痕迹的走「关闭/合并」。
// - 事务内删行：工单本体 + 回复 + 活动 + 关联(link) + 附件行，并把「被合并进本单」的指针清空，
//   避免别的工单列表里挂着「并入了不存在的单」。
// - 附件的实际存储对象在事务提交后尽力删除：失败只记日志（孤儿对象可由存储侧清理回收，
//   而行已删除的附件下载链接不再可达，不会泄漏数据）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"nexadmin/core/response"
)

func (s *Service) DeleteTicket(ctx context.Context, ticketNo string) error {
	ticketNo = strings.TrimSpace(ticketNo)
	var doomed Ticket
	var atts []Attachment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("ticket_no = ?", ticketNo).Take(&doomed).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return response.NotFound("ticket.not_found")
			}
			return err
		}
		id := int64(doomed.ID)
		if err := tx.Where("ticket_id = ?", id).Delete(&TicketReply{}).Error; err != nil {
			return err
		}
		if err := tx.Where("ticket_id = ?", id).Delete(&TicketActivity{}).Error; err != nil {
			return err
		}
		// 关联是双向成对列：两边都可能命中，一条语句带走。
		if err := tx.Where("ticket_a_id = ? OR ticket_b_id = ?", id, id).Delete(&TicketLink{}).Error; err != nil {
			return err
		}
		// 先取附件 key（事务外删对象用），再删行。
		if err := tx.Where("ticket_id = ?", id).Find(&atts).Error; err != nil {
			return err
		}
		if err := tx.Where("ticket_id = ?", id).Delete(&Attachment{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&mergeTicket{}).Where("merged_into_id = ?", id).Update("merged_into_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&Ticket{}, id).Error
	})
	if err != nil {
		return err
	}
	if s.store != nil {
		for _, a := range atts {
			if a.StorageKey == "" {
				continue
			}
			if err := s.store.Delete(ctx, a.StorageKey); err != nil && s.log != nil {
				s.log.Warn("ticket: storage object leak after ticket delete", "key", a.StorageKey)
			}
		}
	}
	return nil
}
