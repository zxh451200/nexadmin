-- 客户主数据补国籍 / 国家/地区：酒店、商城场景要按护照国籍识别客人。
-- 001-003 已执行，按「只增不改」走新文件。两列都存 ISO 3166-1 alpha-2 代码，空串 = 未登记。
ALTER TABLE plg_crm_customer ADD COLUMN country VARCHAR(2) NOT NULL DEFAULT '';
ALTER TABLE plg_crm_customer ADD COLUMN nationality VARCHAR(2) NOT NULL DEFAULT '';
