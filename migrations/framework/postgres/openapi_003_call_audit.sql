-- 开放 API 调用审计：仅保存元数据，不保存凭证、签名、查询串或请求响应正文。
CREATE TABLE sys_openapi_audit (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  principal_type VARCHAR(8) NOT NULL,
  credential_id BIGINT NOT NULL DEFAULT 0,
  credential_name VARCHAR(100) NOT NULL DEFAULT '',
  credential_label VARCHAR(64) NOT NULL DEFAULT '',
  user_id BIGINT NOT NULL DEFAULT 0,
  username VARCHAR(100) NOT NULL DEFAULT '',
  auth_outcome VARCHAR(16) NOT NULL,
  failure_key VARCHAR(128) NOT NULL DEFAULT '',
  method VARCHAR(16) NOT NULL,
  path VARCHAR(500) NOT NULL,
  status_code INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  client_ip VARCHAR(64) NOT NULL DEFAULT '',
  user_agent VARCHAR(500) NOT NULL DEFAULT '',
  request_id VARCHAR(64) NOT NULL DEFAULT '',
  requested_at TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_openapi_audit_time ON sys_openapi_audit (tenant_id, requested_at);
CREATE INDEX idx_sys_openapi_audit_cred ON sys_openapi_audit (tenant_id, principal_type, credential_id, requested_at);
CREATE INDEX idx_sys_openapi_audit_status ON sys_openapi_audit (tenant_id, status_code, requested_at);
CREATE INDEX idx_sys_openapi_audit_cleanup ON sys_openapi_audit (requested_at);
