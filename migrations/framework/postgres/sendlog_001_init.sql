-- 发送日志：短信 / 邮件 / 机器人的投递结果。收件人脱敏存储；不存验证码、正文和密钥；错误信息已截断到 500 字
CREATE TABLE sys_send_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  channel VARCHAR(16) NOT NULL,
  scene VARCHAR(32) NOT NULL,
  recipient VARCHAR(128) NOT NULL DEFAULT '',
  subject VARCHAR(200) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  error VARCHAR(500) NOT NULL DEFAULT '',
  duration_ms INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_send_log_tenant ON sys_send_log (tenant_id, created_at);
CREATE INDEX idx_sys_send_log_status ON sys_send_log (tenant_id, status, created_at);
