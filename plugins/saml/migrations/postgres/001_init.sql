-- SAML 2.0 SP：每租户可配多个企业 IdP。SP 私钥以密文存放。只增不改。
CREATE TABLE plg_saml_source (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  idp_metadata TEXT,
  sp_cert TEXT,
  sp_key_enc TEXT,
  attr_username VARCHAR(64) NOT NULL DEFAULT '',
  attr_email VARCHAR(64) NOT NULL DEFAULT '',
  attr_nickname VARCHAR(64) NOT NULL DEFAULT '',
  auto_create BOOLEAN NOT NULL DEFAULT FALSE,
  match_existing BOOLEAN NOT NULL DEFAULT FALSE,
  default_role VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_saml_source_tenant ON plg_saml_source (tenant_id, enabled);
