ALTER TABLE plg_scim_group ALTER COLUMN external_id DROP NOT NULL;
UPDATE plg_scim_group SET external_id = NULL WHERE external_id = '';
