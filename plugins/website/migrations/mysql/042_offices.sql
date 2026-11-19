-- 办公地点：联系页渲染地点卡与地图（列表 JSON 文本，[]Office{Name,Address,Lng,Lat}）。
-- 地图厂商按访客语言判定（zh 系→天地图，其他→谷歌），Key 配置在「系统设置 → 地图服务」。
ALTER TABLE plg_website_setting ADD COLUMN offices TEXT NULL;
