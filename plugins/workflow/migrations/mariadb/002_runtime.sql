-- 工作流 002：定义保留建模器 XML、实例增加定义快照与乐观锁、待办与动作物化表。
-- 只增不改：001 已应用的库结构不动，新列/新表都在这里。
ALTER TABLE plg_workflow_def ADD COLUMN bpmn_xml MEDIUMTEXT NULL;

ALTER TABLE plg_workflow_inst ADD COLUMN def_version INT NOT NULL DEFAULT 0;
ALTER TABLE plg_workflow_inst ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE plg_workflow_inst ADD COLUMN def_snapshot MEDIUMTEXT NULL;

CREATE INDEX idx_plg_workflow_inst_submitter ON plg_workflow_inst (tenant_id, submitter, status);

CREATE TABLE plg_workflow_task (
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  inst_id BIGINT NOT NULL,
  def_key VARCHAR(64) NOT NULL DEFAULT '',
  node_key VARCHAR(64) NOT NULL,
  node_name VARCHAR(100) NOT NULL DEFAULT '',
  assignee VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_workflow_task_todo ON plg_workflow_task (tenant_id, assignee, status, created_at);
CREATE INDEX idx_plg_workflow_task_inst ON plg_workflow_task (tenant_id, inst_id, status);

CREATE TABLE plg_workflow_action (
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  inst_id BIGINT NOT NULL,
  def_key VARCHAR(64) NOT NULL DEFAULT '',
  node_key VARCHAR(64) NOT NULL DEFAULT '',
  actor VARCHAR(64) NOT NULL DEFAULT '',
  action VARCHAR(16) NOT NULL,
  comment TEXT,
  occurred_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_plg_workflow_action_mine ON plg_workflow_action (tenant_id, actor, action, occurred_at);
CREATE INDEX idx_plg_workflow_action_inst ON plg_workflow_action (tenant_id, inst_id);
