-- 导航多级：parent_id 指向上级导航（0 = 顶级），后台最多维护 3 级。
ALTER TABLE plg_website_nav ADD COLUMN parent_id BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_plg_website_nav_parent ON plg_website_nav (parent_id);
