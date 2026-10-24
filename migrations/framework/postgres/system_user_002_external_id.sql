ALTER TABLE sys_user ADD COLUMN external_id VARCHAR(255) NOT NULL DEFAULT '';
CREATE INDEX idx_sys_user_external_id ON sys_user (tenant_id, external_id);
