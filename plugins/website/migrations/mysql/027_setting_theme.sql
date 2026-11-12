-- 公开站主题：Setting.Theme 指向主题插件注册的模板包 id；空 = 内置 default 主题。
-- 主题插件被卸载时渲染自动回退 default，站点不断线。
ALTER TABLE plg_website_setting ADD COLUMN theme VARCHAR(64) NOT NULL DEFAULT '';
