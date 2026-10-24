-- 表单构建器插件：plg_formbuilder_form（表单与草稿）、plg_formbuilder_version（不可变发布版本）、plg_formbuilder_submission（提交）。
-- 只增不改；后续变更用 002_*.sql。JSON 一律存 TEXT（上限由应用层控制在 60000 字节内，兼容 MySQL TEXT）。
CREATE TABLE plg_formbuilder_form (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  draft_def TEXT,
  draft_rev INT NOT NULL DEFAULT 1,
  published_rev INT NOT NULL DEFAULT 0,
  published_version INT NOT NULL DEFAULT 0,
  published_at DATETIME(3) NULL,
  audience TEXT,
  allow_repeat TINYINT(1) NOT NULL DEFAULT 0,
  deadline_at DATETIME(3) NULL,
  max_submissions INT NOT NULL DEFAULT 0,
  submission_count BIGINT NOT NULL DEFAULT 0,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_formbuilder_form_tenant ON plg_formbuilder_form (tenant_id, status);

CREATE TABLE plg_formbuilder_version (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  form_id BIGINT NOT NULL,
  version INT NOT NULL,
  definition TEXT NOT NULL,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_formbuilder_version ON plg_formbuilder_version (tenant_id, form_id, version);

CREATE TABLE plg_formbuilder_submission (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  form_id BIGINT NOT NULL,
  version INT NOT NULL,
  user_id BIGINT NOT NULL,
  username VARCHAR(64) NOT NULL DEFAULT '',
  data TEXT NOT NULL,
  dedupe_key VARCHAR(128) NOT NULL,
  idem_key VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_plg_formbuilder_sub_dedupe ON plg_formbuilder_submission (tenant_id, form_id, dedupe_key);
CREATE INDEX idx_plg_formbuilder_sub_form ON plg_formbuilder_submission (tenant_id, form_id, id);
CREATE INDEX idx_plg_formbuilder_sub_user ON plg_formbuilder_submission (tenant_id, user_id);
