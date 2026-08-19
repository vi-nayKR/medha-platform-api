-- +goose Up
-- The in-app update feature (app_versions table, apprelease bounded context,
-- and the medha-releases MinIO bucket) has been removed entirely.
DROP INDEX IF EXISTS idx_app_versions_latest;
DROP TABLE IF EXISTS app_versions;

-- +goose Down
CREATE TABLE app_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform        VARCHAR(10)  NOT NULL DEFAULT 'android',
    version_name    VARCHAR(20)  NOT NULL,
    version_code    INTEGER      NOT NULL,
    apk_url         TEXT         NOT NULL,
    apk_size_bytes  BIGINT,
    release_notes   TEXT,
    is_force_update BOOLEAN      NOT NULL DEFAULT false,
    is_active       BOOLEAN      NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    UNIQUE (platform, version_code)
);

CREATE INDEX idx_app_versions_latest
    ON app_versions (platform, is_active, version_code DESC);
