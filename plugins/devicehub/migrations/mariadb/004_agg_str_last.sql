-- 字符串型读数（MQTT 裸字符串帧等）原来被 sweeper 连原始行一起删掉且不聚合，没有长期历史。
-- 小时桶补一列「该小时内最后一条字符串读数」，与数值 min/max/avg 同寿命。只增不改。
ALTER TABLE plg_devhub_reading_agg ADD COLUMN str_last VARCHAR(255) NOT NULL DEFAULT '';
