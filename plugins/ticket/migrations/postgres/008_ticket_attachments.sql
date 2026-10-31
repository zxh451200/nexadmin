-- 工单附件：坐席与访客上传，token 不可枚举，内部备注附件对访客隐藏。只增不改。
CREATE TABLE plg_ticket_attachment (
  id BIGINT NOT NULL,
  token VARCHAR(40) NOT NULL,
  ticket_id BIGINT NOT NULL,
  uploader_type VARCHAR(16) NOT NULL,
  uploader_name VARCHAR(64) NOT NULL DEFAULT '',
  internal BOOLEAN NOT NULL DEFAULT FALSE,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(128) NOT NULL,
  size BIGINT NOT NULL,
  storage_key VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_ticket_attachment_token ON plg_ticket_attachment (token);
CREATE INDEX idx_plg_ticket_attachment_ticket ON plg_ticket_attachment (ticket_id, internal);
