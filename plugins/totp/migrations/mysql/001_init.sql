-- 两步验证插件:plg_totp_secret(租户级,一行一用户)。secret 经 core/secret 加密存储。
-- 只增不改;后续变更用 002_*.sql。
CREATE TABLE plg_totp_secret (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  secret_enc VARCHAR(200) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 0,
  last_step BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_totp_user (tenant_id, user_id)
);
