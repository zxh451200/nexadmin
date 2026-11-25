-- 005：异步入库进度。上传只建档（pending），切块与向量化在后台完成；
-- stage 展示「切分中 / 向量化 12/40 块」，成功后清空。只增不改。
ALTER TABLE plg_kb_document ADD COLUMN stage VARCHAR(128) NOT NULL DEFAULT '';
