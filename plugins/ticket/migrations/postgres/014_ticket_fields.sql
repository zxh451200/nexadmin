-- 自定义字段：管理员定义的附加字段（text/number/select/date/bool），值以 JSON 存在工单的
-- custom_values 列。删除字段定义不清洗存量工单里的值（只是不再显示），因此这里不做级联。
-- options 存逗号分隔候选项；PG 侧 bool 必须是 BOOLEAN（SMALLINT 会让 pgx 启动即败），
-- TEXT 给 DEFAULT 才能让 ALTER 在已有行上通过。只增不改。
CREATE TABLE plg_ticket_field (
  id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL,
  label VARCHAR(64) NOT NULL,
  type VARCHAR(16) NOT NULL DEFAULT '',
  options TEXT NOT NULL DEFAULT '',
  required BOOLEAN NOT NULL DEFAULT FALSE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort INTEGER NOT NULL DEFAULT 0,
  group_id BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_ticket_field_code ON plg_ticket_field (code);
CREATE INDEX idx_plg_ticket_field_sort ON plg_ticket_field (sort, id);
ALTER TABLE plg_ticket ADD COLUMN custom_values TEXT NOT NULL DEFAULT '{}';
