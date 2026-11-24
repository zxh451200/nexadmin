-- 最后一次采集错误的文案与时间：离线设备重启后也能在设备列表/详情看到原因，不必翻服务器日志。
-- 采集恢复时清空（只表达「当前仍未恢复的错误」，历史看采集日志的内存缓冲）。只增不改。
ALTER TABLE plg_devhub_device ADD COLUMN last_error VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plg_devhub_device ADD COLUMN last_error_at TIMESTAMPTZ NULL;
