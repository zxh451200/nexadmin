CREATE TABLE plg_i18n_editor_entry (
  id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, lang VARCHAR(32) NOT NULL,
  "key" VARCHAR(255) NOT NULL, value VARCHAR(2000) NOT NULL, status VARCHAR(16) NOT NULL,
  version INT NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL,
  UNIQUE (tenant_id, lang, "key")
);
CREATE TABLE plg_i18n_editor_version (
  id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, lang VARCHAR(32) NOT NULL,
  version INT NOT NULL, entries TEXT NOT NULL, created_at TIMESTAMP NOT NULL,
  UNIQUE (tenant_id, lang, version)
);
