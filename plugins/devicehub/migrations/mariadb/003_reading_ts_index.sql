-- 读数表按时间扫描的索引：每小时 sweeper（downsampleOnce）按 ts 找过期原始行做降采样后删除。
-- 只增不改。
CREATE INDEX idx_plg_devhub_reading_ts ON plg_devhub_reading (ts);
