-- 设备接入与监控：设备、测点、时序读数（原始 + 降采样）、指令下发、阈值告警。只增不改。
-- 平台级表（plg_devhub_*，同 ticket：设备资产不随租户隔离），协议差异收敛在 device.config JSON 与测点 address 里。

CREATE TABLE plg_devhub_device (
  id BIGINT NOT NULL,
  device_no VARCHAR(40) NOT NULL,
  name VARCHAR(100) NOT NULL,
  protocol VARCHAR(16) NOT NULL, -- modbus-tcp | mqtt
  endpoint VARCHAR(255) NOT NULL DEFAULT '', -- host:port 或 broker 地址
  config TEXT NOT NULL, -- 协议特定 JSON：unitId/pollIntervalMs/username/password/upstreamPrefix
  status VARCHAR(16) NOT NULL DEFAULT 'offline', -- online | offline | disabled
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  last_seen_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_devhub_device_no (device_no)
);
CREATE INDEX idx_plg_devhub_device_status ON plg_devhub_device (enabled, status);

CREATE TABLE plg_devhub_point (
  id BIGINT NOT NULL,
  device_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  address VARCHAR(128) NOT NULL, -- modbus: hr:40001 / ir:30001 / co:1 / di:1；mqtt: 相对上行 topic
  unit VARCHAR(16) NOT NULL DEFAULT '',
  rw VARCHAR(8) NOT NULL DEFAULT 'r', -- r | rw（rw 才允许指令下发）
  data_type VARCHAR(16) NOT NULL DEFAULT 'float32', -- bool | int16 | uint16 | int32 | float32
  scale DOUBLE NOT NULL DEFAULT 1, -- 显示值 = raw / scale（定点缩放）
  poll_interval_ms INT NOT NULL DEFAULT 0, -- 0 = 用设备默认
  deadband DOUBLE NOT NULL DEFAULT 0, -- 与上次存储值的偏差 ≤ deadband 不落库（0 = 全存）
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  sort INT NOT NULL DEFAULT 0,
  last_value_num DOUBLE NULL,
  last_value_str VARCHAR(255) NOT NULL DEFAULT '',
  last_ts DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_devhub_point_addr (device_id, address)
);
CREATE INDEX idx_plg_devhub_point_device ON plg_devhub_point (device_id, enabled, sort);

CREATE TABLE plg_devhub_reading (
  id BIGINT NOT NULL,
  point_id BIGINT NOT NULL,
  device_id BIGINT NOT NULL,
  ts DATETIME(3) NOT NULL,
  num_value DOUBLE NULL,
  str_value VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_devhub_reading_point ON plg_devhub_reading (point_id, ts);
CREATE INDEX idx_plg_devhub_reading_device ON plg_devhub_reading (device_id, ts);

CREATE TABLE plg_devhub_reading_agg (
  id BIGINT NOT NULL,
  point_id BIGINT NOT NULL,
  bucket DATETIME(3) NOT NULL, -- 桶起始时间（小时）
  num_min DOUBLE NOT NULL,
  num_max DOUBLE NOT NULL,
  num_avg DOUBLE NOT NULL,
  cnt INT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_devhub_agg (point_id, bucket)
);

CREATE TABLE plg_devhub_command (
  id BIGINT NOT NULL,
  device_id BIGINT NOT NULL,
  point_id BIGINT NOT NULL,
  raw_value VARCHAR(64) NOT NULL, -- 发给驱动的原始值（已按 scale 还原）
  status VARCHAR(16) NOT NULL DEFAULT 'queued', -- queued | done | failed | timeout
  result VARCHAR(255) NOT NULL DEFAULT '',
  operator_id BIGINT NOT NULL DEFAULT 0,
  operator_name VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  done_at DATETIME(3) NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_devhub_command_status ON plg_devhub_command (status, id);
CREATE INDEX idx_plg_devhub_command_device ON plg_devhub_command (device_id, id);

CREATE TABLE plg_devhub_alarm_rule (
  id BIGINT NOT NULL,
  point_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  op VARCHAR(8) NOT NULL, -- gt | lt | ge | le | eq | ne
  threshold DOUBLE NOT NULL,
  level VARCHAR(8) NOT NULL DEFAULT 'warn', -- info | warn | critical
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  cooldown_seconds INT NOT NULL DEFAULT 300, -- 触发后冷却期内不重复发事件
  last_fired_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_devhub_alarm_rule_point ON plg_devhub_alarm_rule (point_id, enabled);

CREATE TABLE plg_devhub_alarm_event (
  id BIGINT NOT NULL,
  rule_id BIGINT NOT NULL,
  device_id BIGINT NOT NULL,
  point_id BIGINT NOT NULL,
  value DOUBLE NOT NULL,
  level VARCHAR(8) NOT NULL,
  message VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_devhub_alarm_event_point ON plg_devhub_alarm_event (point_id, created_at);
