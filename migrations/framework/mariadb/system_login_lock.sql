-- 登录失败锁定（按租户 + 用户名一行）：缓存是快路径，这张表兜住进程重启与多实例。
-- 用户名不存在也照建行：锁定状态对真/假账号一视同仁，避免靠锁定行为枚举账号。
CREATE TABLE sys_login_lock (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  username VARCHAR(64) NOT NULL,
  locked_until DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_login_lock_tenant_username ON sys_login_lock (tenant_id, username);
CREATE INDEX idx_sys_login_lock_until ON sys_login_lock (locked_until);
