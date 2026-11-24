-- 导航槽位：header=顶部导航（历史行为），footer=页脚导航。
-- 页脚按「顶级项 = 一列标题、子项 = 该列链接」渲染；存量行由默认值回填成 header，
-- 服务层再把空值一并归一到 header，所以升级后站点外观不变。
ALTER TABLE plg_website_nav ADD COLUMN `position` varchar(16) NOT NULL DEFAULT 'header';
