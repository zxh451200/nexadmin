-- 媒体优化插件：视频托管配置（单行 key/value）与视频资产台账。
CREATE TABLE plg_media_setting (
    config_key VARCHAR(64) NOT NULL PRIMARY KEY,
    config_value TEXT NOT NULL
);

CREATE TABLE plg_media_video (
    id BIGINT NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    size BIGINT NOT NULL,
    provider VARCHAR(32) NOT NULL,
    video_id VARCHAR(64) NOT NULL,
    playback_url VARCHAR(500) NOT NULL,
    status VARCHAR(20) NOT NULL,
    uploaded_by VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_plg_media_video_tenant ON plg_media_video (tenant_id, created_at);
