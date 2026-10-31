-- 工单分组与成员：组队列 + 自动分派。只增不改。
CREATE TABLE plg_ticket_group (
  id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL,
  description VARCHAR(200) NOT NULL DEFAULT '',
  auto_assign BOOLEAN NOT NULL DEFAULT FALSE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_ticket_group_sort ON plg_ticket_group (sort, id);

CREATE TABLE plg_ticket_group_member (
  id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  user_name VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_ticket_group_member ON plg_ticket_group_member (group_id, user_id);
CREATE INDEX idx_plg_ticket_group_member_user ON plg_ticket_group_member (user_id);

ALTER TABLE plg_ticket ADD COLUMN group_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE plg_ticket ADD COLUMN group_name VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX idx_plg_ticket_group ON plg_ticket (group_id, status, last_reply_at);
