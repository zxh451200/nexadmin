-- FAQ 分类：轻量分组标签，空串=未分类（老数据不改）。
ALTER TABLE plg_website_faq
  ADD COLUMN `category` varchar(64) NOT NULL DEFAULT '' AFTER `answer`,
  ADD INDEX `idx_plg_website_faq_category` (`category`);
