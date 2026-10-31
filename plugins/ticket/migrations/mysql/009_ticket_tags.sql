-- 工单标签：逗号包裹的规范化串（,vip,refund,），筛选按边界匹配。只增不改。
ALTER TABLE plg_ticket ADD COLUMN tags VARCHAR(255) NOT NULL DEFAULT '';
