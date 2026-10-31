-- 工单坐席协作：待客户回复（SLA 停表）、内部备注、解决/关闭时间、创建人、快捷回复。只增不改。
ALTER TABLE plg_ticket ADD COLUMN sla_paused_at TIMESTAMP NULL;
ALTER TABLE plg_ticket ADD COLUMN resolved_at TIMESTAMP NULL;
ALTER TABLE plg_ticket ADD COLUMN closed_at TIMESTAMP NULL;
ALTER TABLE plg_ticket ADD COLUMN creator_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_ticket ADD COLUMN creator_name VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE plg_ticket_reply ADD COLUMN is_internal BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_plg_ticket_assignee ON plg_ticket (assignee_id, status, last_reply_at);

CREATE TABLE plg_ticket_canned (
  id BIGINT NOT NULL,
  title VARCHAR(100) NOT NULL,
  content VARCHAR(2000) NOT NULL,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_ticket_canned_sort ON plg_ticket_canned (sort, id);
