-- 菜单表（平台级，不带 tenant_id）。主键为应用侧生成的雪花 ID。
-- parent_id = 0 表示顶级；code 是出厂数据的稳定业务键（按钮/菜单用权限码，目录用 dir:路径）。
CREATE TABLE sys_menu (
  id BIGINT NOT NULL,
  parent_id BIGINT NOT NULL,
  code VARCHAR(100) NOT NULL,
  type VARCHAR(10) NOT NULL,
  title VARCHAR(100) NOT NULL,
  icon VARCHAR(64) NOT NULL,
  path VARCHAR(255) NOT NULL,
  component VARCHAR(255) NOT NULL,
  perm VARCHAR(100) NOT NULL,
  sort INT NOT NULL,
  keep_alive TINYINT(1) NOT NULL,
  hidden TINYINT(1) NOT NULL,
  external TINYINT(1) NOT NULL,
  status VARCHAR(2) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_menu_parent ON sys_menu (parent_id);
CREATE INDEX idx_sys_menu_code ON sys_menu (code);
