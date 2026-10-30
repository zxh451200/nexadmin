-- 我的公告「清空」：记每个用户的清空时间点，之前发布的公告不再出现在该用户的列表与未读数里
CREATE TABLE sys_notice_clear (
  id BIGINT NOT NULL,
  tenant_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  cleared_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_notice_clear ON sys_notice_clear (tenant_id, user_id);
