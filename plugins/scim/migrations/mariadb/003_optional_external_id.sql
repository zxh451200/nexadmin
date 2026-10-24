ALTER TABLE plg_scim_group MODIFY COLUMN external_id VARCHAR(255) NULL;
UPDATE plg_scim_group SET external_id = NULL WHERE external_id = '';
