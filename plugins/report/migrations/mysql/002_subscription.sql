-- 报表订阅：到点把报表的更新提醒发给收件人列表（站内/邮件文本摘要，附件另立项）。
-- 每张报表一条订阅配置（report_id 唯一）；last_sent_date 用于「当天已发就跳过」的去重。
CREATE TABLE plg_report_subscription (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  report_id BIGINT NOT NULL,
  schedule VARCHAR(16) NOT NULL,
  send_hour SMALLINT NOT NULL DEFAULT 8,
  weekday SMALLINT NOT NULL DEFAULT 1,
  recipients TEXT NOT NULL,
  enabled SMALLINT NOT NULL DEFAULT 1,
  last_sent_date DATE NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_report_subscription_report (report_id)
);
CREATE INDEX idx_plg_report_subscription_due ON plg_report_subscription (enabled, schedule, send_hour);
