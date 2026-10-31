-- 工单满意度（CSAT）：报告人在已解决/已关闭后评 1~5 分。只增不改。
ALTER TABLE plg_ticket ADD COLUMN rating TINYINT NULL;
ALTER TABLE plg_ticket ADD COLUMN rating_comment VARCHAR(500) NOT NULL DEFAULT '';
ALTER TABLE plg_ticket ADD COLUMN rated_at DATETIME(3) NULL;
