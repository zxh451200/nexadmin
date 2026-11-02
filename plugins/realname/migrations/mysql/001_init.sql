-- 证件实名插件:plg_realname_config(租户级一行,在线核验密钥经 core/secret 加密存储) +
-- plg_realname_verification(核验台账,只存姓名/号码的脱敏串与号码摘要,不落明文)。
-- 只增不改;后续变更用 002_*.sql。
CREATE TABLE plg_realname_config (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 0,
  provider VARCHAR(32) NOT NULL DEFAULT '',
  endpoint VARCHAR(512) NOT NULL DEFAULT '',
  app_key VARCHAR(128) NOT NULL DEFAULT '',
  app_secret_enc VARCHAR(1024) NOT NULL DEFAULT '',
  timeout_ms INT NOT NULL DEFAULT 8000,
  daily_limit INT NOT NULL DEFAULT 200,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_realname_config_tenant (tenant_id)
);
CREATE TABLE plg_realname_verification (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name_masked VARCHAR(64) NOT NULL DEFAULT '',
  id_type VARCHAR(16) NOT NULL DEFAULT 'id_card',
  number_masked VARCHAR(64) NOT NULL DEFAULT '',
  number_hash VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'valid',
  reason VARCHAR(32) NOT NULL DEFAULT '',
  provider VARCHAR(32) NOT NULL DEFAULT '',
  provider_msg VARCHAR(200) NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(64) NOT NULL DEFAULT '',
  remark VARCHAR(200) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_plg_realname_hash (tenant_id, number_hash),
  KEY idx_plg_realname_status (tenant_id, status)
);
