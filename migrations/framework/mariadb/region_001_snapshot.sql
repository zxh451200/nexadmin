-- 省市区在线同步快照：单行（id=1），payload 是完整三级树的紧凑 JSON。
-- 没有行时用随包发布的内置数据；同步和恢复内置都只动这一行。
CREATE TABLE sys_region_snapshot (
  id BIGINT NOT NULL,
  source_url VARCHAR(512) NOT NULL,
  version VARCHAR(64) NOT NULL,
  data_date VARCHAR(16) NOT NULL,
  license VARCHAR(128) NOT NULL,
  payload MEDIUMTEXT NOT NULL,
  sha256 CHAR(64) NOT NULL,
  provinces INT NOT NULL,
  prefectures INT NOT NULL,
  counties INT NOT NULL,
  synced_by VARCHAR(64) NOT NULL,
  synced_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
