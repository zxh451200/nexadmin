-- 客户主数据插件 P0：plg_crm_contact（联系人，客户内主联系人由服务层事务保证唯一）、
-- plg_crm_activity（跟进记录时间线，todo 用 done/next_at 表待办）。只增不改。
CREATE TABLE plg_crm_contact (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  customer_id BIGINT NOT NULL DEFAULT 0,
  name VARCHAR(100) NOT NULL,
  title VARCHAR(100) NOT NULL DEFAULT '',
  phone VARCHAR(32) NOT NULL DEFAULT '',
  email VARCHAR(128) NOT NULL DEFAULT '',
  is_primary BOOLEAN NOT NULL DEFAULT FALSE,
  remark VARCHAR(500) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_crm_contact_customer ON plg_crm_contact (tenant_id, customer_id);

CREATE TABLE plg_crm_activity (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  customer_id BIGINT NOT NULL DEFAULT 0,
  opportunity_id BIGINT NOT NULL DEFAULT 0,
  kind VARCHAR(16) NOT NULL DEFAULT 'note',
  content VARCHAR(2000) NOT NULL,
  next_at VARCHAR(16) NOT NULL DEFAULT '',
  done BOOLEAN NOT NULL DEFAULT FALSE,
  author VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_crm_activity_customer ON plg_crm_activity (tenant_id, customer_id, created_at);
CREATE INDEX idx_crm_activity_todo ON plg_crm_activity (tenant_id, kind, done, next_at);
