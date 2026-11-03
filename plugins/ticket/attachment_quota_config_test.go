package ticket

import (
	"testing"

	"nexadmin/core/config"
)

// 附件配额的注入链：config.yaml 的 ticket 段 → applyAttachmentQuota → attachmentQuota()。
// 「没装配置」和「装了但键缺省」必须落到同一组出厂默认，否则换个部署文件就悄悄改了上限。
// 配额本身的放行/拒绝边界已由 attachment_test.go 的 count / bytes 两条用例钉住。
func TestApplyAttachmentQuota(t *testing.T) {
	t.Cleanup(func() { SetAttachmentQuota(0, 0) })

	withTicket := func(count, mb int) *config.Config {
		cfg := &config.Config{}
		cfg.Ticket.AttachmentMaxCount = count
		cfg.Ticket.AttachmentMaxMB = mb
		return cfg
	}

	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		wantCount int64
		wantBytes int64
	}{
		{name: "nil 配置 = 出厂默认", cfg: nil, wantCount: 20, wantBytes: 100 << 20},
		{name: "键缺省 = 出厂默认", cfg: &config.Config{}, wantCount: 20, wantBytes: 100 << 20},
		{name: "按配置收紧", cfg: withTicket(5, 10), wantCount: 5, wantBytes: 10 << 20},
		{name: "按配置放宽", cfg: withTicket(200, 2048), wantCount: 200, wantBytes: 2048 << 20},
		{name: "只配份数，字节回默认", cfg: withTicket(9, 0), wantCount: 9, wantBytes: 100 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applyAttachmentQuota(tc.cfg)
			gotCount, gotBytes := attachmentQuota()
			if gotCount != tc.wantCount || gotBytes != tc.wantBytes {
				t.Errorf("配额 = %d 份 / %d 字节，想要 %d 份 / %d 字节", gotCount, gotBytes, tc.wantCount, tc.wantBytes)
			}
		})
	}
}
