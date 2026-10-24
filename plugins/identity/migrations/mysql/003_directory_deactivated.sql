-- 目录同步：记录本次因“已不在目录里”而停用的账号数（deactivateMissing）。只增不改。
ALTER TABLE plg_identity_directory_run ADD COLUMN deactivated INT NOT NULL DEFAULT 0;
