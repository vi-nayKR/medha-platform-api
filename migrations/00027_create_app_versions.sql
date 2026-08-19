-- +goose Up
CREATE TABLE app_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    platform        VARCHAR(10)  NOT NULL DEFAULT 'android',
    version_name    VARCHAR(20)  NOT NULL,           -- "1.0.1" — shown to user
    version_code    INTEGER      NOT NULL,           -- 2 — used for comparison
    apk_url         TEXT         NOT NULL,           -- MinIO object URL
    apk_size_bytes  BIGINT,                          -- shown in download dialog
    release_notes   TEXT,                            -- "Bug fixes, Panchanga improvements"
    is_force_update BOOLEAN      NOT NULL DEFAULT false,
    is_active       BOOLEAN      NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    UNIQUE (platform, version_code)
);

CREATE INDEX idx_app_versions_latest
    ON app_versions (platform, is_active, version_code DESC);

INSERT INTO app_versions (
    platform, version_name, version_code,
    apk_url, apk_size_bytes, release_notes, is_force_update
) VALUES (
    'android', '1.0.0', 1,
    'http://your-server-ip:9000/medha-releases/android/medha-1.0.0.apk',
    0,
    'Initial release',
    false
);

-- +goose Down
DROP INDEX IF EXISTS idx_app_versions_latest;
DROP TABLE IF EXISTS app_versions;
