-- 工单分组与成员：组队列 + 自动分派。只增不改。
CREATE TABLE plg_ticket_group (
  id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL,
  description VARCHAR(200) NOT NULL DEFAULT '',
  auto_assign TINYINT(1) NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_ticket_group_sort ON plg_ticket_group (sort, id);

CREATE TABLE plg_ticket_group_member (
  id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  user_name VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_ticket_group_member (group_id, user_id)
);
CREATE INDEX idx_plg_ticket_group_member_user ON plg_ticket_group_member (user_id);

ALTER TABLE plg_ticket ADD COLUMN group_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_ticket ADD COLUMN group_name VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX idx_plg_ticket_group ON plg_ticket (group_id, status, last_reply_at);
