-- 工单动态：状态、负责人、分组、优先级、标签、附件、评价与规则命中的变更轨迹。
-- 与回复分表：回复是「说的话」，动态是「发生的事」，混在一张表里未读数、摘要与访客可见性都会变形。只增不改。
CREATE TABLE plg_ticket_activity (
  id BIGINT NOT NULL,
  ticket_id BIGINT NOT NULL,
  ticket_no VARCHAR(40) NOT NULL DEFAULT '',
  actor_type VARCHAR(16) NOT NULL,
  actor_id VARCHAR(64) NOT NULL DEFAULT '',
  actor_name VARCHAR(64) NOT NULL DEFAULT '',
  action VARCHAR(32) NOT NULL,
  from_value VARCHAR(64) NOT NULL DEFAULT '',
  to_value VARCHAR(64) NOT NULL DEFAULT '',
  detail VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_plg_ticket_activity_ticket (ticket_id, id)
);
