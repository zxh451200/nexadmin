-- 合并重复工单 + 关联单。只增不改：已有表的既有列一个不动。
-- 被合并方只加一个可空指针列：合并是一次性事实，「合并进了哪张」写在单上就够，
-- 反查「这张主单吞了哪些」是一条 WHERE，不值得再开一张表；主单的「合并了 N 张」不建计数列，
-- 即时反查后落进动态轨迹，免得再多一个其他写入路径不会同步的漂移计数。
-- 回复与附件一律不搬：原地保留才可追溯，「不再活跃」由终态 + 指针表达。
-- 关联单是双向、可解除的多对多 → 新表按（较小 id, 较大 id）规范化只存一行，唯一键天然去重。
ALTER TABLE plg_ticket ADD COLUMN merged_into_id BIGINT NULL;
CREATE INDEX idx_plg_ticket_merged_into ON plg_ticket (merged_into_id);

CREATE TABLE plg_ticket_link (
  id BIGINT NOT NULL,
  ticket_a_id BIGINT NOT NULL,
  ticket_b_id BIGINT NOT NULL,
  link_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_ticket_link_pair ON plg_ticket_link (ticket_a_id, ticket_b_id);
CREATE INDEX idx_plg_ticket_link_a ON plg_ticket_link (ticket_a_id, id);
CREATE INDEX idx_plg_ticket_link_b ON plg_ticket_link (ticket_b_id, id);
