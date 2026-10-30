-- 我的公告「删除」：每条公告按用户记一行移除记录，公告本身与其他人不受影响
CREATE TABLE sys_notice_dismiss (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  notice_id BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_notice_dismiss ON sys_notice_dismiss (tenant_id, user_id, notice_id);
