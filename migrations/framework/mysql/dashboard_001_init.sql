-- 工作台布局：每个用户一行，layout 为 {"order":[...],"hidden":[...]} 的 JSON 文本
CREATE TABLE sys_dashboard_layout (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  layout TEXT NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_dashboard_layout_user ON sys_dashboard_layout (tenant_id, user_id);
