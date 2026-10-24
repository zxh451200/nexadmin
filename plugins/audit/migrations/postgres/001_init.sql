-- 数据变更审计插件：plg_audit_config（审计范围，每租户一行）、plg_audit_log（变更记录）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_audit_config (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  table_list TEXT NOT NULL,
  updated_by VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_audit_config_tenant ON plg_audit_config (tenant_id);

CREATE TABLE plg_audit_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  table_name VARCHAR(128) NOT NULL,
  row_pk VARCHAR(255) NOT NULL,
  action VARCHAR(16) NOT NULL,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(100) NOT NULL DEFAULT '',
  fields VARCHAR(500) NOT NULL DEFAULT '',
  changes TEXT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_audit_log_time ON plg_audit_log (tenant_id, created_at);
CREATE INDEX idx_plg_audit_log_table ON plg_audit_log (tenant_id, table_name, created_at);
CREATE INDEX idx_plg_audit_log_row ON plg_audit_log (tenant_id, table_name, row_pk);
CREATE INDEX idx_plg_audit_log_operator ON plg_audit_log (tenant_id, operator_id, created_at);
