-- 自定义字段：管理员定义的附加字段（text/number/select/date/bool），值以 JSON 存在客户的
-- custom_values 列。删除字段定义不清洗存量客户里的值（只是不再显示），因此这里不做级联。
-- options 存逗号分隔候选项；MySQL 8.0.12 不给 TEXT 列写 DEFAULT，写侧必须显式带值。只增不改。
-- 字段定义是平台级表（全租户共享，无 tenant_id，与工单字段同口径）。001-007 已执行，走新文件。
CREATE TABLE plg_crm_field (
  id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL,
  label VARCHAR(64) NOT NULL,
  type VARCHAR(16) NOT NULL,
  options TEXT NOT NULL,
  required TINYINT(1) NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_crm_field_code (code)
);
CREATE INDEX idx_plg_crm_field_sort ON plg_crm_field (sort, id);
ALTER TABLE plg_crm_customer ADD COLUMN custom_values TEXT NOT NULL;
