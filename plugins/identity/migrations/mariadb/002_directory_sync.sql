CREATE TABLE plg_identity_directory_run (
  source_id BIGINT NOT NULL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  job_id BIGINT NOT NULL,
  scanned INT NOT NULL DEFAULT 0,
  created INT NOT NULL DEFAULT 0,
  updated INT NOT NULL DEFAULT 0,
  complete BOOLEAN NOT NULL DEFAULT FALSE
);
