-- 工作流插件：plg_workflow_def（流程定义，租户级）、plg_workflow_inst（流程实例，租户级）。
-- state_json 存引擎状态快照（bpmn.Instance）。只增不改；后续变更用 002_*.sql。
CREATE TABLE plg_workflow_def (
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  def_key VARCHAR(64) NOT NULL,
  name VARCHAR(100) NOT NULL,
  status VARCHAR(16) NOT NULL,
  version INT NOT NULL DEFAULT 1,
  definition TEXT,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_workflow_def_tenant ON plg_workflow_def (tenant_id, def_key);

CREATE TABLE plg_workflow_inst (
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  def_id BIGINT NOT NULL,
  def_key VARCHAR(64) NOT NULL,
  title VARCHAR(200) NOT NULL DEFAULT '',
  biz_type VARCHAR(32) NOT NULL DEFAULT '',
  business_key VARCHAR(64) NOT NULL DEFAULT '',
  submitter VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  state_json TEXT,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_workflow_inst_tenant ON plg_workflow_inst (tenant_id, status, def_key);
CREATE INDEX idx_plg_workflow_inst_def ON plg_workflow_inst (tenant_id, def_id);
CREATE INDEX idx_plg_workflow_inst_biz ON plg_workflow_inst (biz_type, business_key, status);
