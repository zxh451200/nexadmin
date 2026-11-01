-- 商机挂主联系人（可选；contact_id=0 表示未指定）。002 已执行，按「只增不改」走新文件。
ALTER TABLE plg_crm_opportunity ADD COLUMN contact_id BIGINT NOT NULL DEFAULT 0;
