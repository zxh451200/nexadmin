-- 完善 Web SSO 配置：元数据来源 URL、组映射、IdP-initiated、请求签名与 NameID 格式。
-- 属性名放宽到 255：OASIS / WS-Federation 等 IdP 的声明名常是完整 URI，64 放不下。
ALTER TABLE plg_saml_source
  ADD COLUMN idp_metadata_url VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN attr_groups VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN attr_phone VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN group_role_mappings TEXT,
  ADD COLUMN require_mapped_group TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN allow_idp_initiated TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN sign_authn_request TINYINT(1) NOT NULL DEFAULT 0,
  ADD COLUMN name_id_format VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE plg_saml_source
  MODIFY COLUMN attr_username VARCHAR(255) NOT NULL DEFAULT '',
  MODIFY COLUMN attr_email VARCHAR(255) NOT NULL DEFAULT '',
  MODIFY COLUMN attr_nickname VARCHAR(255) NOT NULL DEFAULT '';
