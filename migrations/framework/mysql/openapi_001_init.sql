-- 开放平台：sys_pat（个人访问令牌，只存 sha256 哈希与前 8 位）、sys_openapi_app（开放应用/服务账号，secret 为 core/secret 密文）、
-- sys_webhook（Webhook 订阅，secret 为 core/secret 密文）、sys_webhook_delivery（投递记录，保留 30 天后自动清理）
CREATE TABLE sys_pat (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  scope VARCHAR(16) NOT NULL,
  token_hash VARCHAR(64) NOT NULL,
  prefix VARCHAR(16) NOT NULL,
  last_used_at DATETIME(3) NULL,
  expires_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_pat_hash ON sys_pat (token_hash);
CREATE INDEX idx_sys_pat_user ON sys_pat (tenant_id, user_id);

CREATE TABLE sys_openapi_app (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  app_key VARCHAR(64) NOT NULL,
  secret_enc TEXT NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  role_ids TEXT,
  ip_allow TEXT,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  last_used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_openapi_app_key ON sys_openapi_app (app_key);
CREATE INDEX idx_sys_openapi_app_tenant ON sys_openapi_app (tenant_id, enabled);

CREATE TABLE sys_webhook (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  url VARCHAR(500) NOT NULL,
  events TEXT,
  secret_enc TEXT NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_webhook_tenant ON sys_webhook (tenant_id, enabled);

CREATE TABLE sys_webhook_delivery (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  webhook_id BIGINT NOT NULL,
  event_id VARCHAR(64) NOT NULL,
  event VARCHAR(64) NOT NULL,
  payload MEDIUMTEXT,
  status VARCHAR(16) NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  next_attempt_at DATETIME(3) NOT NULL,
  response_status INT NOT NULL DEFAULT 0,
  response_body TEXT,
  error_msg VARCHAR(500) NOT NULL DEFAULT '',
  duration_ms INT NOT NULL DEFAULT 0,
  delivered_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_webhook_delivery_hook ON sys_webhook_delivery (webhook_id, created_at);
CREATE INDEX idx_sys_webhook_delivery_due ON sys_webhook_delivery (status, next_attempt_at);
CREATE INDEX idx_sys_webhook_delivery_created ON sys_webhook_delivery (created_at);
CREATE INDEX idx_sys_webhook_delivery_tenant ON sys_webhook_delivery (tenant_id);
