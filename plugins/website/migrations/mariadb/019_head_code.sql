-- 站点自定义 <head> 代码：统计脚本（GA/百度统计）、Search Console 验证等。
-- 仅 website:setting 权限可写（可信管理员），模板按原样注入 <head>；限长由服务层控制。
ALTER TABLE plg_website_setting ADD COLUMN head_code TEXT NULL;
