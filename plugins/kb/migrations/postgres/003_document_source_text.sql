-- 003：文档原文。重新切分（改分块参数）以原文为输入，不再靠「把切块拼回去 + 猜重叠」——
-- 周期性文本下猜重叠会多剥内容。只增不改；老库该列为 NULL，代码回退到拼接切块。
ALTER TABLE plg_kb_document ADD COLUMN source_text TEXT;
