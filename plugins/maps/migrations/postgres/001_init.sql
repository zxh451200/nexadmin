-- 地图服务插件：三家厂商的 Key 配置（浏览器 Key 下发；服务端 Key 仅供代理使用）。
CREATE TABLE plg_maps_setting (
    config_key VARCHAR(64) NOT NULL PRIMARY KEY,
    config_value TEXT NOT NULL
);
