-- 图片元数据：宽高在 tus 完成时用 image.DecodeConfig 读头部提取（0 = 非图片或未提取）。
-- 预览与管理端据此展示「宽 × 高」，变体副本继承原文件尺寸。
ALTER TABLE sys_file ADD COLUMN width INT NOT NULL DEFAULT 0;
ALTER TABLE sys_file ADD COLUMN height INT NOT NULL DEFAULT 0;
