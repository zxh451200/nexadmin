-- 恢复码表：验证器丢失时的自救通道（一次一码，原子防重放）。只增不改。
CREATE TABLE plg_totp_recovery (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  code_hash VARCHAR(64) NOT NULL,
  used_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  CONSTRAINT pk_plg_totp_recovery PRIMARY KEY (id),
  CONSTRAINT uk_plg_totp_recovery UNIQUE (tenant_id, user_id, code_hash)
);
