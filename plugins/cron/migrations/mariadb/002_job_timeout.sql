-- 按任务配置单次尝试的超时（秒，0 = 用全局默认 5 分钟）：慢任务不再拖全局常量的后腿。
ALTER TABLE plg_cron_job ADD COLUMN timeout_seconds INT NOT NULL DEFAULT 0;
