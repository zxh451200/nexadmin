-- 多 IdP 打包元数据：记录选用的 Entity ID；记录元数据最近一次成功同步时间。
ALTER TABLE plg_saml_source
  ADD COLUMN idp_entity_id VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN metadata_refreshed_at TIMESTAMP(3) NULL;
