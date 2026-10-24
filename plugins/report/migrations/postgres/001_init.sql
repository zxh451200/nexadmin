-- 报表中心插件：plg_report_dataset / chart / report / screen / screen_role。只增不改；后续变更用 002_*.sql。
-- 配置 JSON 存 TEXT；所有表带 tenant_id。
CREATE TABLE plg_report_dataset (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  source VARCHAR(64) NOT NULL,
  config TEXT,
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_report_dataset_tenant ON plg_report_dataset (tenant_id, name);

CREATE TABLE plg_report_chart (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  dataset_id BIGINT NOT NULL,
  type VARCHAR(16) NOT NULL,
  config TEXT,
  remark VARCHAR(255) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_report_chart_tenant ON plg_report_chart (tenant_id, dataset_id);

CREATE TABLE plg_report_report (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  group_name VARCHAR(64) NOT NULL DEFAULT '',
  remark VARCHAR(255) NOT NULL DEFAULT '',
  config TEXT,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_report_report_tenant ON plg_report_report (tenant_id, group_name);

CREATE TABLE plg_report_screen (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  remark VARCHAR(255) NOT NULL DEFAULT '',
  config TEXT,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_report_screen_tenant ON plg_report_screen (tenant_id, name);

CREATE TABLE plg_report_screen_role (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  screen_id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_report_screen_role_screen ON plg_report_screen_role (tenant_id, screen_id);
CREATE INDEX idx_plg_report_screen_role_role ON plg_report_screen_role (tenant_id, role_id);
