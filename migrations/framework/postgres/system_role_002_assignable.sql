-- 角色是否允许被下级管理员分配给用户（内置超管角色由代码固定视为不可分配，不靠迁移改值）。
ALTER TABLE sys_role ADD COLUMN assignable BOOLEAN NOT NULL DEFAULT TRUE;
