-- 新闻菜单的后台路径 /website/posts → /website/news，与新路由一致。
-- 菜单播种只补不改（core/seed 按 code 判断），已装实例必须在这里改旧行，
-- 否则侧栏仍指向被删掉的 /website/posts。条件同时带 code 与旧 path，避免误伤手工改过的菜单。
UPDATE sys_menu SET path = '/website/news' WHERE code = 'website:post' AND path = '/website/posts';
