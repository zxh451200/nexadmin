-- 字典（类型与数据，平台级表）接入回收站：加软删除列，并把两条唯一索引降级成普通索引（名字保留，语义已变）。
-- 取舍与写法同 system_post_002_soft_delete.sql：唯一性改由 dict.typeCodeTaken / dict.checkData 保证，
-- 还原撞码由回收站在还原前显式检查并回 409 recycle.restore_conflict。
ALTER TABLE sys_dict_type ADD COLUMN deleted_at DATETIME(3) NULL;
CREATE INDEX idx_sys_dict_type_deleted ON sys_dict_type (deleted_at);
DROP INDEX uk_sys_dict_type_code ON sys_dict_type;
CREATE INDEX uk_sys_dict_type_code ON sys_dict_type (code);
ALTER TABLE sys_dict_data ADD COLUMN deleted_at DATETIME(3) NULL;
CREATE INDEX idx_sys_dict_data_deleted ON sys_dict_data (deleted_at);
DROP INDEX uk_sys_dict_data_type_value ON sys_dict_data;
CREATE INDEX uk_sys_dict_data_type_value ON sys_dict_data (type_id, dict_value);
