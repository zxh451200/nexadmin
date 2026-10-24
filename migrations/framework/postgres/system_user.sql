-- 用户表（按租户隔离，软删除）、用户-角色、用户-岗位。
-- password 存 bcrypt 哈希；空字符串表示“不可登录”（出厂演示账号，需管理员先重置密码）。
CREATE TABLE sys_user (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  username VARCHAR(64) NOT NULL,
  password VARCHAR(100) NOT NULL,
  nickname VARCHAR(64) NOT NULL,
  avatar VARCHAR(500) NOT NULL,
  phone VARCHAR(20) NOT NULL,
  email VARCHAR(100) NOT NULL,
  gender VARCHAR(2) NOT NULL,
  dept_id BIGINT NOT NULL,
  status VARCHAR(2) NOT NULL,
  province VARCHAR(12) NOT NULL,
  city VARCHAR(12) NOT NULL,
  district VARCHAR(12) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  is_super BOOLEAN NOT NULL,
  last_login_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  deleted_at TIMESTAMP(3) NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_user_tenant_username ON sys_user (tenant_id, username);
CREATE INDEX idx_sys_user_dept ON sys_user (dept_id);

CREATE TABLE sys_user_role (
  user_id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_sys_user_role_role ON sys_user_role (role_id);

CREATE TABLE sys_user_post (
  user_id BIGINT NOT NULL,
  post_id BIGINT NOT NULL,
  PRIMARY KEY (user_id, post_id)
);
