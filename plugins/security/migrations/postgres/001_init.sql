-- 高级安全插件：plg_security_totp（二次验证绑定 / 宽限期）、plg_security_recovery（恢复码哈希）、
-- plg_security_policy（强制策略，每租户一行）、plg_security_ip_rule（IP 黑白名单）、plg_security_log（管理操作日志）。
-- 只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_security_totp (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  username VARCHAR(100) NOT NULL DEFAULT '',
  secret_enc TEXT,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  enabled_at TIMESTAMP(3) NULL,
  grace_until TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_security_totp_user ON plg_security_totp (tenant_id, user_id);

CREATE TABLE plg_security_recovery (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  code_hash VARCHAR(64) NOT NULL,
  used_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_security_recovery_user ON plg_security_recovery (tenant_id, user_id, code_hash);

CREATE TABLE plg_security_policy (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  mode VARCHAR(16) NOT NULL DEFAULT 'optional',
  roles VARCHAR(1000) NOT NULL DEFAULT '',
  grace_days INT NOT NULL DEFAULT 7,
  updated_by VARCHAR(100) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_security_policy_tenant ON plg_security_policy (tenant_id);

CREATE TABLE plg_security_ip_rule (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  action VARCHAR(8) NOT NULL,
  cidr VARCHAR(64) NOT NULL,
  scope VARCHAR(8) NOT NULL DEFAULT 'all',
  remark VARCHAR(200) NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_security_ip_rule_tenant ON plg_security_ip_rule (tenant_id, enabled);

CREATE TABLE plg_security_log (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(100) NOT NULL DEFAULT '',
  action VARCHAR(32) NOT NULL,
  target VARCHAR(100) NOT NULL DEFAULT '',
  detail VARCHAR(500) NOT NULL DEFAULT '',
  ip VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_security_log_time ON plg_security_log (tenant_id, created_at);
