-- 派生文件：WebP 副本等衍生资源挂在原文件下（variant_of 指向原文件 id），
-- 列表不展示、随原文件删除；原文件本身永不覆盖。
-- 0 = 普通文件。
ALTER TABLE sys_file ADD COLUMN variant_of BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_sys_file_variant ON sys_file (variant_of);
