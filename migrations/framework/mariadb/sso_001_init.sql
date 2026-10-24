-- 单点登录（OIDC Provider）：sys_sso_app（接入应用，secret 只存加盐哈希）、sys_sso_refresh（refresh_token 哈希，轮换用）
CREATE TABLE sys_sso_app (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  client_id VARCHAR(64) NOT NULL,
  secret_hash VARCHAR(200) NOT NULL,
  redirect_uris TEXT,
  logo VARCHAR(500) NOT NULL DEFAULT '',
  home_url VARCHAR(500) NOT NULL DEFAULT '',
  description VARCHAR(500) NOT NULL DEFAULT '',
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  role_ids TEXT,
  allow_org_read TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_sso_app_client ON sys_sso_app (client_id);
CREATE INDEX idx_sys_sso_app_tenant ON sys_sso_app (tenant_id, enabled);

CREATE TABLE sys_sso_refresh (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  app_id BIGINT NOT NULL,
  client_id VARCHAR(64) NOT NULL,
  user_id BIGINT NOT NULL,
  family_id VARCHAR(32) NOT NULL,
  token_hash VARCHAR(64) NOT NULL,
  scope VARCHAR(200) NOT NULL,
  auth_time DATETIME(3) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_sso_refresh_hash ON sys_sso_refresh (token_hash);
CREATE INDEX idx_sys_sso_refresh_user ON sys_sso_refresh (client_id, user_id);
CREATE INDEX idx_sys_sso_refresh_family ON sys_sso_refresh (family_id);
CREATE INDEX idx_sys_sso_refresh_app ON sys_sso_refresh (app_id);
CREATE INDEX idx_sys_sso_refresh_expires ON sys_sso_refresh (expires_at);
