-- 语言启用开关：停用的语言保留全部配置（前缀/日期格式/文案）但不参与协商与路由，
-- 比删行可逆。表注释里「没有启用开关」的旧语义由本迁移废止。
ALTER TABLE sys_language ADD COLUMN enabled TINYINT(1) NOT NULL DEFAULT 1;
