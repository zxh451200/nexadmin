-- 生命周期策略默认保持兼容：空组不收权；外部 ID 精确匹配需显式开启。
ALTER TABLE plg_saml_source
  ADD COLUMN revoke_roles_on_empty_groups TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN match_provisioned_external_id TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN attr_external_id VARCHAR(255) NOT NULL DEFAULT '';
