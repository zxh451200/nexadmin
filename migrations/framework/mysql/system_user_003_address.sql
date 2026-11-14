-- 详细地址：省市区编码之外的自由文本（路名门牌等）；完整地址由前端用区划树 + address 推导，不落库。
ALTER TABLE sys_user ADD COLUMN address VARCHAR(255) NOT NULL DEFAULT '';
