-- AI 网关插件（收敛方案第 4 步）：plg_aigateway_model（模型接入配置，密钥加密）、
-- plg_aigateway_usage（按租户 token 用量台账）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_aigateway_model (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  kind VARCHAR(16) NOT NULL,
  name VARCHAR(200) NOT NULL,
  provider VARCHAR(64) NOT NULL DEFAULT '',
  base_url VARCHAR(500) NOT NULL DEFAULT '',
  model VARCHAR(255) NOT NULL DEFAULT '',
  api_key_enc VARCHAR(1024) NOT NULL DEFAULT '',
  dimension INT NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  is_default TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_aigateway_model_kind ON plg_aigateway_model (tenant_id, kind, enabled);

CREATE TABLE plg_aigateway_usage (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  model_id BIGINT NOT NULL DEFAULT 0,
  kind VARCHAR(16) NOT NULL,
  provider VARCHAR(64) NOT NULL DEFAULT '',
  model VARCHAR(255) NOT NULL DEFAULT '',
  prompt_tokens BIGINT NOT NULL DEFAULT 0,
  completion_tokens BIGINT NOT NULL DEFAULT 0,
  total_tokens BIGINT NOT NULL DEFAULT 0,
  biz_type VARCHAR(32) NOT NULL DEFAULT '',
  biz_no VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_aigateway_usage_tenant ON plg_aigateway_usage (tenant_id, kind, created_at);
CREATE INDEX idx_aigateway_usage_model ON plg_aigateway_usage (model_id);
