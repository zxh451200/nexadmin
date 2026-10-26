-- LDAP 登录适配插件:plg_ldap_config(平台级单行配置)。密码经 core/secret 加密存储。
-- 只增不改;后续变更用 002_*.sql。
CREATE TABLE plg_ldap_config (
  id BIGINT PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  host VARCHAR(200) NOT NULL DEFAULT '',
  port INT NOT NULL DEFAULT 389,
  use_tls BOOLEAN NOT NULL DEFAULT FALSE,
  start_tls BOOLEAN NOT NULL DEFAULT FALSE,
  bind_dn VARCHAR(400) NOT NULL DEFAULT '',
  bind_password VARCHAR(600) NOT NULL DEFAULT '',
  base_dn VARCHAR(400) NOT NULL DEFAULT '',
  user_filter VARCHAR(400) NOT NULL DEFAULT '(uid=%s)',
  attr_username VARCHAR(64) NOT NULL DEFAULT 'uid',
  attr_email VARCHAR(64) NOT NULL DEFAULT 'mail',
  attr_nickname VARCHAR(64) NOT NULL DEFAULT 'cn',
  match_existing BOOLEAN NOT NULL DEFAULT FALSE,
  auto_create BOOLEAN NOT NULL DEFAULT FALSE,
  default_role VARCHAR(64) NOT NULL DEFAULT '',
  timeout_ms INT NOT NULL DEFAULT 5000,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
