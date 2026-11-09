-- 元数据信任：可选钉住内网 CA（TLS）与 IdP 元数据签名证书；SP Entity ID 可按 IdP 要求覆盖。
ALTER TABLE plg_saml_source
  ADD COLUMN metadata_ca_cert TEXT,
  ADD COLUMN metadata_sign_cert TEXT,
  ADD COLUMN sp_entity_id VARCHAR(500) NOT NULL DEFAULT '';
