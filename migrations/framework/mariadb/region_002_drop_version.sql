-- 快照表不再保存 version：随包的内置数据版本由代码常量决定，落库只留 data_date 与 sha256。
-- 001 建表时带过这一列（已在库的环境里它一直没人写值），所以这里删掉，不改 001。
ALTER TABLE sys_region_snapshot DROP COLUMN version;
