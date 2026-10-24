-- 身份源插件：plg_identity_source（每租户可配多个 LDAP / CAS 身份源）。只增不改；后续变更用 002_*.sql。
-- config 是配置 JSON；LDAP 绑定口令在其中以 enc:v1: 密文存放，不会明文落库。
CREATE TABLE plg_identity_source (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  type VARCHAR(16) NOT NULL,
  name VARCHAR(100) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  config TEXT,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_identity_source_tenant ON plg_identity_source (tenant_id, type, enabled);
