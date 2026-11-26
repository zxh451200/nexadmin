-- 只有 SAML 实际创建的 sys_user_role 才允许被对应来源回收。
-- 历史授予记录无法证明归属，默认保守保留，不把已有手工授权认领为 SAML 授权。
-- role_id 固定授权身份，角色改名后仍可准确收权；无法唯一匹配的历史记录保持 0 且不可回收。
ALTER TABLE plg_saml_role_grant
  ADD COLUMN role_id BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN owns_assignment TINYINT(1) NOT NULL DEFAULT 0;

CREATE INDEX idx_plg_saml_role_grant_role ON plg_saml_role_grant (tenant_id, user_id, role_id, source_id);
