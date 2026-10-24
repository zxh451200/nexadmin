CREATE TABLE plg_scim_token (
  id BIGINT NOT NULL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  hash VARCHAR(64) NOT NULL,
  revoked BOOLEAN NOT NULL DEFAULT FALSE,
  last_used TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL,
  UNIQUE (hash),
  INDEX idx_plg_scim_token_tenant (tenant_id)
);
