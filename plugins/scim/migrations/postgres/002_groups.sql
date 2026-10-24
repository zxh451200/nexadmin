CREATE TABLE plg_scim_group (
  id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, external_id VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL,
  UNIQUE (tenant_id, external_id)
);
CREATE TABLE plg_scim_group_member (
  group_id BIGINT NOT NULL, user_id BIGINT NOT NULL, tenant_id BIGINT NOT NULL,
  PRIMARY KEY (group_id, user_id)
);
