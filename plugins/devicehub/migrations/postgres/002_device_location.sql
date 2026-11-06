-- 设备安装位置：经纬度（WGS-84 存储原值，前端按地图厂商转换显示）与可读地址。
-- 由 LocationField 选点或手填；详情页与宿主 API（Device/Points）都会带出。只增不改。
ALTER TABLE plg_devhub_device ADD COLUMN lng DOUBLE PRECISION NULL;
ALTER TABLE plg_devhub_device ADD COLUMN lat DOUBLE PRECISION NULL;
ALTER TABLE plg_devhub_device ADD COLUMN location VARCHAR(255) NOT NULL DEFAULT '';
