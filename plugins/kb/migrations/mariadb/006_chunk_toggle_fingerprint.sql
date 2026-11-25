-- 006：切块启停 + 文档内容指纹。禁用的块不参与检索但保留可恢复；
-- 同内容重传覆盖原文档，避免重复入库稀释 topK。只增不改。
ALTER TABLE plg_kb_chunk ADD COLUMN enabled TINYINT(1) NOT NULL DEFAULT 1;
ALTER TABLE plg_kb_document ADD COLUMN fingerprint VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX idx_plg_kb_doc_fp ON plg_kb_document (fingerprint);
