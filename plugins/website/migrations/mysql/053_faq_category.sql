-- FAQ 分类：轻量分组标签，空串=未分类（老数据不改）。
-- 不做独立分类表：FAQ 是首页板块的小量内容，分组只需要一个可筛选/可展示的标签，
-- 分类名走用户输入原文（与问题/答案同口径的展示字段，不单独翻译）。
ALTER TABLE plg_website_faq
  ADD COLUMN `category` varchar(64) NOT NULL DEFAULT '' AFTER `answer`,
  ADD INDEX `idx_plg_website_faq_category` (`category`);
