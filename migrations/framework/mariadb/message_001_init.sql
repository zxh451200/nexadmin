-- 站内消息：一条消息一个收件人
CREATE TABLE sys_message (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  category VARCHAR(32) NOT NULL,
  title VARCHAR(200) NOT NULL,
  content TEXT,
  link VARCHAR(500),
  read_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_message_user ON sys_message (tenant_id, user_id, read_at);
CREATE INDEX idx_sys_message_created ON sys_message (created_at);
