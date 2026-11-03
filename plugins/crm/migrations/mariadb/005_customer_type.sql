-- 客户分「个人 / 企业」：住客与商城会员是自然人，签约的是单位。类型决定生效字段组——
-- 个人用证件类型+证件号（配合已有的国籍），企业用统一社会信用代码+行业+注册地址；
-- 另一组由服务层清空。存量客户默认 person（历史锚点都来自 member 体系）。
-- 001-004 已执行，按「只增不改」走新文件。
ALTER TABLE plg_crm_customer ADD COLUMN type VARCHAR(16) NOT NULL DEFAULT 'person';
ALTER TABLE plg_crm_customer ADD COLUMN id_type VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE plg_crm_customer ADD COLUMN id_no VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_crm_customer ADD COLUMN tax_no VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE plg_crm_customer ADD COLUMN industry VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_crm_customer ADD COLUMN address VARCHAR(200) NOT NULL DEFAULT '';
CREATE INDEX idx_crm_customer_type ON plg_crm_customer (tenant_id, type);
