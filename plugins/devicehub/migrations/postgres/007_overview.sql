-- 总览页：在线率历史快照 + 告警窗口/状态聚合索引。只增不改。
-- device.status 只有当前值，历史在线率必须靠小时采样留存；桶唯一，重启/多实例幂等覆盖。
CREATE TABLE plg_devhub_status_sample (
  id BIGINT NOT NULL,
  bucket TIMESTAMP NOT NULL,
  total INT NOT NULL DEFAULT 0,
  online INT NOT NULL DEFAULT 0,
  offline INT NOT NULL DEFAULT 0,
  disabled INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_plg_devhub_status_sample_bucket UNIQUE (bucket)
);

-- 总览按时间窗扫告警趋势；现有索引都以 point_id/device_id 开头，服务不了全局窗口。
CREATE INDEX idx_plg_devhub_alarm_event_created ON plg_devhub_alarm_event (created_at, level, status);
-- 当前未处理告警按级别聚合。
CREATE INDEX idx_plg_devhub_alarm_event_status_level ON plg_devhub_alarm_event (status, level);
