-- 字典类型与字典数据（平台级，不带 tenant_id）。
CREATE TABLE sys_dict_type (
  id BIGINT NOT NULL,
  name VARCHAR(100) NOT NULL,
  code VARCHAR(64) NOT NULL,
  sort INT NOT NULL,
  status VARCHAR(1) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_dict_type_code ON sys_dict_type (code);

CREATE TABLE sys_dict_data (
  id BIGINT NOT NULL,
  type_id BIGINT NOT NULL,
  dict_label VARCHAR(100) NOT NULL,
  label_en VARCHAR(100) NOT NULL,
  dict_value VARCHAR(100) NOT NULL,
  tag VARCHAR(20) NOT NULL,
  sort INT NOT NULL,
  status VARCHAR(1) NOT NULL,
  remark VARCHAR(500) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE UNIQUE INDEX uk_sys_dict_data_type_value ON sys_dict_data (type_id, dict_value);
