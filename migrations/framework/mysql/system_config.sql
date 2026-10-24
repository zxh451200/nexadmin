-- 系统参数（平台级 KV）与设置页分区配置（每个分区一行 JSON）。
CREATE TABLE sys_config (
  id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  config_key VARCHAR(128) NOT NULL,
  config_value TEXT NOT NULL,
  config_type VARCHAR(10) NOT NULL,
  is_public TINYINT(1) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_config_key ON sys_config (config_key);

CREATE TABLE sys_setting (
  id BIGINT NOT NULL,
  section VARCHAR(32) NOT NULL,
  content TEXT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_setting_section ON sys_setting (section);
