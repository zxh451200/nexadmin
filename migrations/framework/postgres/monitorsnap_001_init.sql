-- 监控历史快照：服务/缓存/接口监控按固定周期各落一行扁平指标（payload 是 JSON 对象），
-- 保留 7 天由后台清理任务删除；查询按 (kind, ts) 走索引。
CREATE TABLE sys_monitor_snapshot (
  id BIGINT NOT NULL,
  kind VARCHAR(16) NOT NULL,
  ts TIMESTAMP(3) NOT NULL,
  payload TEXT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_monitor_snapshot_kind_ts ON sys_monitor_snapshot (kind, ts);
CREATE INDEX idx_sys_monitor_snapshot_purge ON sys_monitor_snapshot (ts);
