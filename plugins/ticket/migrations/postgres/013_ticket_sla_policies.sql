-- SLA 从「整仓一套」升到多套命名策略 + 节假日：策略行加 code/label/holidays，
-- code 空串即既有那一行（默认策略），老库不改数据也能直接跑；分组加 sla_code 绑定策略。
-- holidays 存逗号分隔的 YYYY-MM-DD（读侧逐项校验后丢弃脏值）。
-- PG 侧给 TEXT 补 DEFAULT ''，否则已有行的 ALTER 会因 NOT NULL 无默认值而失败。只增不改。
ALTER TABLE plg_ticket_sla ADD COLUMN code VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE plg_ticket_sla ADD COLUMN label VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_ticket_sla ADD COLUMN holidays TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_plg_ticket_sla_code ON plg_ticket_sla (code, id);
ALTER TABLE plg_ticket_group ADD COLUMN sla_code VARCHAR(32) NOT NULL DEFAULT '';
