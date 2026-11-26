-- 新账号在 MFA 挑战期间只建号、不授角色；持久化待初始化标记，挑战中断后的下一次登录仍会补做首次角色初始化。
CREATE TABLE plg_saml_role_init (
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (tenant_id, user_id, source_id)
);
CREATE INDEX idx_plg_saml_role_init_user ON plg_saml_role_init (user_id, source_id);
