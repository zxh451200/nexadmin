-- 文件模块：分组与文件。主键为应用侧生成的雪花 ID（BIGINT，无自增）；时间统一存 UTC。
CREATE TABLE sys_file_group (
    id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(100) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX idx_sys_file_group_tenant ON sys_file_group (tenant_id);
CREATE TABLE sys_file (
    id BIGINT NOT NULL,
    tenant_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    storage_path VARCHAR(500) NOT NULL,
    url TEXT NOT NULL,
    poster_url TEXT NOT NULL,
    mime_type VARCHAR(150) NOT NULL,
    category VARCHAR(20) NOT NULL,
    size BIGINT NOT NULL,
    hash VARCHAR(64) NOT NULL,
    visibility VARCHAR(10) NOT NULL,
    token VARCHAR(32) NOT NULL,
    uploader VARCHAR(100) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX idx_sys_file_tenant_group ON sys_file (tenant_id, group_id);
CREATE INDEX idx_sys_file_token ON sys_file (token);
CREATE INDEX idx_sys_file_created ON sys_file (tenant_id, created_at);
