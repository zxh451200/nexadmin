-- 自定义字段：管理员定义的附加字段（text/number/select/date/bool），值以 JSON 存在工单的
-- custom_values 列。删除字段定义不清洗存量工单里的值（只是不再显示），因此这里不做级联。
-- options 存逗号分隔候选项；MariaDB 与 MySQL 一样不给 TEXT 列写 DEFAULT，写侧必须显式带值。只增不改。
CREATE TABLE plg_ticket_field (
  id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL,
  label VARCHAR(64) NOT NULL,
  type VARCHAR(16) NOT NULL,
  options TEXT NOT NULL,
  required TINYINT(1) NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  group_id BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_ticket_field_code (code)
);
CREATE INDEX idx_plg_ticket_field_sort ON plg_ticket_field (sort, id);
ALTER TABLE plg_ticket ADD COLUMN custom_values TEXT NOT NULL;
