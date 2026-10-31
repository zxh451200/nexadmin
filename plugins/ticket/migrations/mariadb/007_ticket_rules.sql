-- 工单自动化规则：新建工单时按条件执行动作。只增不改。
CREATE TABLE plg_ticket_rule (
  id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  match_priority VARCHAR(16) NOT NULL DEFAULT '',
  match_biz_type VARCHAR(32) NOT NULL DEFAULT '',
  match_keyword VARCHAR(64) NOT NULL DEFAULT '',
  set_priority VARCHAR(16) NOT NULL DEFAULT '',
  set_group_id BIGINT NOT NULL DEFAULT 0,
  note VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_ticket_rule_sort ON plg_ticket_rule (sort, id);
