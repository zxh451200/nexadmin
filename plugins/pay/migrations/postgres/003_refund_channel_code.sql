-- 退款单冗余渠道编码：列表页 pending 退款要显示所属渠道，原来每行回查一次原单。
-- 老数据列为空时前端保留回查兜底。只增不改。
ALTER TABLE plg_pay_refund ADD COLUMN channel_code VARCHAR(32) NOT NULL DEFAULT '';
