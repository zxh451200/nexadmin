-- 两步验证插件:plg_totp_secret(租户级,一行一用户)。secret 经 core/secret 加密存储。
-- 只增不改;后续变更用 002_*.sql。
CREATE TABLE plg_totp_secret (
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  secret_enc VARCHAR(200) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  last_step BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_totp_user ON plg_totp_secret (tenant_id, user_id);
