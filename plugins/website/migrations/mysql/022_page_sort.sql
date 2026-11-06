-- 页面排序：Sort 越小越靠前（管理端列表与导航候选按它排）；0 = 未设置，排在最后。
ALTER TABLE plg_website_page ADD COLUMN sort INT NOT NULL DEFAULT 0;
