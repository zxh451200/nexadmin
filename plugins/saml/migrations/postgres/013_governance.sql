-- 固定 SP 对外基址，并补齐配置版本、诊断事件与会话治理。
ALTER TABLE plg_saml_source
  ADD COLUMN external_base_url VARCHAR(500) NOT NULL DEFAULT '';

CREATE TABLE plg_saml_source_version (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL,
  version INT NOT NULL,
  config_json TEXT NOT NULL,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(100) NOT NULL DEFAULT '',
  reason VARCHAR(500) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_plg_saml_source_version UNIQUE (source_id, version)
);
CREATE INDEX idx_plg_saml_source_version_tenant ON plg_saml_source_version (tenant_id, source_id);

CREATE TABLE plg_saml_event (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL,
  type VARCHAR(64) NOT NULL,
  result VARCHAR(16) NOT NULL,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(100) NOT NULL DEFAULT '',
  user_id BIGINT NOT NULL DEFAULT 0,
  correlation_id VARCHAR(100) NOT NULL DEFAULT '',
  ip VARCHAR(64) NOT NULL DEFAULT '',
  detail TEXT,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_saml_event_source ON plg_saml_event (tenant_id, source_id, created_at);
CREATE INDEX idx_plg_saml_event_correlation ON plg_saml_event (correlation_id);

CREATE TABLE plg_saml_session (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  name_id_hash VARCHAR(64) NOT NULL,
  session_index VARCHAR(500) NOT NULL DEFAULT '',
  session_key VARCHAR(64) NOT NULL,
  access_jti VARCHAR(100) NOT NULL DEFAULT '',
  refresh_jti VARCHAR(100) NOT NULL DEFAULT '',
  expires_at TIMESTAMP(3) NOT NULL,
  last_seen_at TIMESTAMP(3) NOT NULL,
  revoked_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_plg_saml_session_key UNIQUE (session_key)
);
CREATE INDEX idx_plg_saml_session_source ON plg_saml_session (tenant_id, source_id, last_seen_at);
CREATE INDEX idx_plg_saml_session_name ON plg_saml_session (source_id, name_id_hash);
