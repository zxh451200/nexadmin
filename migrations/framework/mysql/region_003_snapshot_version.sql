-- 快照版本历史：sys_region_snapshot 是 id=1 的单行表，覆盖保存时旧 payload 随写随丢。
-- 每次覆盖（同步 / 导入 / 回滚 / 恢复内置）前把旧行按内容存进这里，摘要相同不重复存；
-- 改坏了能回看、能一键回滚，回滚再走一次正常校验与落库，版本继续递增、还能再回滚。
-- 与 sys_region_snapshot 一样是平台级单数据集（全平台共用一份区划数据），所以没有 tenant_id。
CREATE TABLE sys_region_snapshot_version (
  id BIGINT NOT NULL,
  version INT NOT NULL,
  payload MEDIUMTEXT NOT NULL,
  sha256 CHAR(64) NOT NULL,
  data_date VARCHAR(16) NOT NULL,
  source_url VARCHAR(512) NOT NULL,
  provinces INT NOT NULL,
  prefectures INT NOT NULL,
  counties INT NOT NULL,
  created_by VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_sys_region_snapshot_version ON sys_region_snapshot_version (version);
CREATE INDEX idx_sys_region_snapshot_version_sha ON sys_region_snapshot_version (sha256);
