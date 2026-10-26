-- 在线客服插件：plg_im_conversation（会话）、plg_im_message（消息）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_im_conversation (
  id BIGINT PRIMARY KEY,
  conv_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL DEFAULT '',
  biz_no VARCHAR(64) NOT NULL DEFAULT '',
  visitor_token VARCHAR(64) NOT NULL,
  visitor_name VARCHAR(64) NOT NULL DEFAULT '',
  subject VARCHAR(200) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  msg_seq BIGINT NOT NULL DEFAULT 0,
  agent_read_seq BIGINT NOT NULL DEFAULT 0,
  last_msg_at TIMESTAMP NULL,
  last_preview VARCHAR(120) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_im_conv_no ON plg_im_conversation (conv_no);
CREATE UNIQUE INDEX uk_plg_im_conv_biz ON plg_im_conversation (biz_type, biz_no, visitor_token);
CREATE INDEX idx_plg_im_conv_status ON plg_im_conversation (status, last_msg_at);

CREATE TABLE plg_im_message (
  id BIGINT PRIMARY KEY,
  conv_id BIGINT NOT NULL,
  seq BIGINT NOT NULL,
  sender_type VARCHAR(16) NOT NULL,
  sender_id VARCHAR(64) NOT NULL DEFAULT '',
  sender_name VARCHAR(64) NOT NULL DEFAULT '',
  content VARCHAR(2000) NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_im_msg_seq ON plg_im_message (conv_id, seq);
CREATE INDEX idx_plg_im_message_conv ON plg_im_message (conv_id, sender_type, seq);
