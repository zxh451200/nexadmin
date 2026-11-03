-- 流程定义版本历史：同 key 编辑一直是 UPSERT 同一行（version+1），旧版本内容随写随丢。
-- 每次覆盖保存前把旧版本快照落到这里，改坏了能回看、也能一键回滚（回滚本身再走一次保存，版本继续递增）。
CREATE TABLE plg_workflow_def_version (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  def_id BIGINT NOT NULL,
  def_key VARCHAR(64) NOT NULL,
  version INT NOT NULL,
  name VARCHAR(100) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT '',
  bpmn_xml TEXT,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_workflow_def_version ON plg_workflow_def_version (tenant_id, def_id, version);
