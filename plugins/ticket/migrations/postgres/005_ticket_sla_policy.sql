-- 工单 SLA 策略：营业时间、按优先级首响/解决时限；工单增加解决时限与违约标记。只增不改。
CREATE TABLE plg_ticket_sla (
  id BIGINT NOT NULL,
  workdays VARCHAR(20) NOT NULL DEFAULT '1,2,3,4,5,6,7',
  start_minute INTEGER NOT NULL DEFAULT 0,
  end_minute INTEGER NOT NULL DEFAULT 1440,
  targets TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id)
);

ALTER TABLE plg_ticket ADD COLUMN resolution_due_at TIMESTAMP NULL;
ALTER TABLE plg_ticket ADD COLUMN resolution_overdue BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_plg_ticket_resolution_due ON plg_ticket (status, resolution_overdue, resolution_due_at);
