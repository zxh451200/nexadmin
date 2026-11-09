-- 登录时按组重算角色：只收回本来源授过的角色，手工授权与其它来源不受影响。
-- 授予记账表让「收回」有依据；没有这一行时任何已有用户的角色都保持现状。
ALTER TABLE plg_saml_source
  ADD COLUMN sync_roles_on_login TINYINT(1) NOT NULL DEFAULT 0;

CREATE TABLE plg_saml_role_grant (
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL,
  role_code VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (tenant_id, user_id, source_id, role_code)
);
CREATE INDEX idx_plg_saml_role_grant_user ON plg_saml_role_grant (user_id, source_id);
