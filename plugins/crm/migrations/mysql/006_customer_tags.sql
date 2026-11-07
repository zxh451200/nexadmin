-- 客户标签：规范化存储为「,a,b,」（与工单标签同一套约定），列表列 + 筛选 + 批量打标。
-- 不建标签字典表：标签就是自由短词（最多 10 个、每个 20 字），字典表带来的管理成本大于收益；
-- 「使用中的标签」清单由服务层从现有行聚合（见 tag.go ListCustomerTags）。
-- 001-005 已执行，按「只增不改」走新文件。
ALTER TABLE plg_crm_customer ADD COLUMN tags VARCHAR(240) NOT NULL DEFAULT '';
