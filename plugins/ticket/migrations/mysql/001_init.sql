-- 工单中心插件：plg_ticket（工单）、plg_ticket_reply（回复）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_ticket (
  id BIGINT NOT NULL,
  ticket_no VARCHAR(40) NOT NULL,
  biz_type VARCHAR(32) NOT NULL DEFAULT '',
  biz_no VARCHAR(64) NOT NULL DEFAULT '',
  title VARCHAR(200) NOT NULL,
  status VARCHAR(16) NOT NULL,
  priority VARCHAR(16) NOT NULL,
  reporter_type VARCHAR(16) NOT NULL,
  reporter_id VARCHAR(64) NOT NULL DEFAULT '',
  reporter_name VARCHAR(64) NOT NULL DEFAULT '',
  assignee_id BIGINT NOT NULL DEFAULT 0,
  assignee_name VARCHAR(64) NOT NULL DEFAULT '',
  reply_seq BIGINT NOT NULL DEFAULT 0,
  first_reply_at DATETIME(3) NULL,
  last_reply_at DATETIME(3) NULL,
  last_replier_type VARCHAR(16) NOT NULL DEFAULT '',
  last_preview VARCHAR(120) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_ticket_no (ticket_no)
);
CREATE INDEX idx_plg_ticket_status ON plg_ticket (status, priority, last_reply_at);
CREATE INDEX idx_plg_ticket_biz ON plg_ticket (biz_type, biz_no);

CREATE TABLE plg_ticket_reply (
  id BIGINT NOT NULL,
  ticket_id BIGINT NOT NULL,
  seq BIGINT NOT NULL,
  replier_type VARCHAR(16) NOT NULL,
  replier_id VARCHAR(64) NOT NULL DEFAULT '',
  replier_name VARCHAR(64) NOT NULL DEFAULT '',
  content VARCHAR(2000) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_ticket_reply_seq (ticket_id, seq)
);
CREATE INDEX idx_plg_ticket_reply_ticket ON plg_ticket_reply (ticket_id, seq);
