-- 工单 SLA：响应到期时间与超时标记。只增不改。
ALTER TABLE plg_ticket ADD COLUMN due_at DATETIME(3) NULL;
ALTER TABLE plg_ticket ADD COLUMN overdue TINYINT(1) NOT NULL DEFAULT 0;
CREATE INDEX idx_plg_ticket_overdue ON plg_ticket (status, overdue, due_at);
