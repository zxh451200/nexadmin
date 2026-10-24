-- 角色表（按租户隔离）、角色-菜单、角色-自定义数据范围部门。
CREATE TABLE sys_role (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(64) NOT NULL,
  code VARCHAR(64) NOT NULL,
  sort INT NOT NULL,
  status VARCHAR(2) NOT NULL,
  data_scope VARCHAR(20) NOT NULL,
  builtin BOOLEAN NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_role_tenant_code ON sys_role (tenant_id, code);

CREATE TABLE sys_role_menu (
  role_id BIGINT NOT NULL,
  menu_id BIGINT NOT NULL,
  PRIMARY KEY (role_id, menu_id)
);
CREATE INDEX idx_sys_role_menu_menu ON sys_role_menu (menu_id);

CREATE TABLE sys_role_dept (
  role_id BIGINT NOT NULL,
  dept_id BIGINT NOT NULL,
  PRIMARY KEY (role_id, dept_id)
);
