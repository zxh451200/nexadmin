-- 客户主数据插件（收敛方案第 3 步最小骨架）：plg_crm_customer（客户主数据）、
-- plg_crm_opportunity（线索/商机台账）。external 锚点 (tenant_id, source, no) 唯一；
-- 可空列允许 NULL 重复（未绑定锚点的手工客户）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_crm_customer (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(200) NOT NULL,
  code VARCHAR(64) NOT NULL DEFAULT '',
  level VARCHAR(16) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'active',
  external_source VARCHAR(16) NULL,
  external_no VARCHAR(64) NULL,
  owner VARCHAR(64) NOT NULL DEFAULT '',
  phone VARCHAR(32) NOT NULL DEFAULT '',
  email VARCHAR(128) NOT NULL DEFAULT '',
  remark VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_crm_customer_external (tenant_id, external_source, external_no)
);
CREATE INDEX idx_crm_customer_tenant ON plg_crm_customer (tenant_id, status);
CREATE INDEX idx_crm_customer_name ON plg_crm_customer (tenant_id, name);

CREATE TABLE plg_crm_opportunity (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  customer_id BIGINT NOT NULL DEFAULT 0,
  title VARCHAR(200) NOT NULL,
  stage VARCHAR(16) NOT NULL DEFAULT 'lead',
  amount_minor BIGINT NOT NULL DEFAULT 0,
  currency VARCHAR(8) NOT NULL DEFAULT 'CNY',
  expected_at VARCHAR(10) NOT NULL DEFAULT '',
  owner VARCHAR(64) NOT NULL DEFAULT '',
  remark VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_crm_opportunity_tenant ON plg_crm_opportunity (tenant_id, stage);
CREATE INDEX idx_crm_opportunity_customer ON plg_crm_opportunity (customer_id);
