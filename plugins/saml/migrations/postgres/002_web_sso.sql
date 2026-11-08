-- 完善 Web SSO 配置：元数据来源 URL、组映射、IdP-initiated、请求签名与 NameID 格式。
-- 属性名放宽到 255：OASIS / WS-Federation 等 IdP 的声明名常是完整 URI，64 放不下。
ALTER TABLE plg_saml_source
  ADD COLUMN idp_metadata_url VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN attr_groups VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN attr_phone VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN group_role_mappings TEXT,
  ADD COLUMN require_mapped_group BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN allow_idp_initiated BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN sign_authn_request BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN name_id_format VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_saml_source
  ALTER COLUMN attr_username TYPE VARCHAR(255),
  ALTER COLUMN attr_email TYPE VARCHAR(255),
  ALTER COLUMN attr_nickname TYPE VARCHAR(255);
