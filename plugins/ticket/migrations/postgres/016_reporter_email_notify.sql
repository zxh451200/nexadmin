-- 客户邮件通知：工单记住报告人邮箱（邮件进单天然有、表单可选填），坐席公开回复 /
-- 解决 / 关闭时按设置通知；plg_ticket_setting 是单行设置（邮件通知开关 + 客服门户地址，
-- 门户地址用于邮件里的「查看工单」链接，空 = 邮件不带链接）。只增不改。
ALTER TABLE plg_ticket ADD COLUMN reporter_email VARCHAR(128) NOT NULL DEFAULT '';
CREATE INDEX idx_plg_ticket_reporter_email ON plg_ticket (reporter_email);

CREATE TABLE plg_ticket_setting (
  id BIGINT NOT NULL,
  email_notify_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  portal_url VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
