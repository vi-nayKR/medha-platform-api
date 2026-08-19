-- +goose Up

-- ============================================================
-- 1. Non-login system publishers (platform-controlled content authors)
-- ============================================================
CREATE TABLE system_publishers (
    id            UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    publisher_key TEXT         NOT NULL UNIQUE,
    display_name  TEXT         NOT NULL,
    username      TEXT         NOT NULL UNIQUE,
    avatar_url    TEXT,
    description   TEXT         NOT NULL DEFAULT '',
    is_verified   BOOLEAN      NOT NULL DEFAULT TRUE,
    is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at    BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE TRIGGER trg_system_publishers_updated_at
    BEFORE UPDATE ON system_publishers
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

INSERT INTO system_publishers (publisher_key, display_name, username, avatar_url, is_verified, is_active)
VALUES ('medha-app', 'Medha', 'medhaapp', 'https://example.com/assets/favicon.svg', TRUE, TRUE);

-- ============================================================
-- 2. Official stories (campaigns, not one row per recipient)
-- ============================================================
CREATE TYPE story_audience_scope AS ENUM (
    'all_users',
    'segment',
    'explicit_users'
);

CREATE TYPE official_story_status AS ENUM (
    'draft',
    'scheduled',
    'active',
    'paused',
    'expired',
    'archived'
);

CREATE TABLE official_stories (
    id                 UUID                  PRIMARY KEY DEFAULT uuid_generate_v4(),
    publisher_id       UUID                  NOT NULL REFERENCES system_publishers(id),
    title              TEXT,
    caption            TEXT,
    media_type         TEXT                  NOT NULL,
    media_url          TEXT                  NOT NULL,
    thumbnail_url      TEXT,
    action_url         TEXT,
    action_label       TEXT,
    audience_scope     story_audience_scope  NOT NULL,
    audience_rules     JSONB                 NOT NULL DEFAULT '{}'::jsonb,
    status             official_story_status NOT NULL DEFAULT 'draft',
    priority           INTEGER               NOT NULL DEFAULT 0,
    starts_at          BIGINT                NOT NULL,
    expires_at         BIGINT                NOT NULL,
    -- This project's admin auth is a separate username/token system, not a
    -- row in `users` — so audit identity is the admin's username, not a FK.
    created_by_admin   TEXT,
    created_at         BIGINT                NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at         BIGINT                NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    published_at       BIGINT,
    deleted_at         BIGINT,
    CHECK (expires_at > starts_at)
);

CREATE TRIGGER trg_official_stories_updated_at
    BEFORE UPDATE ON official_stories
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- Active time-window lookup is the hot path (every story-feed request).
CREATE INDEX idx_official_stories_active_window
    ON official_stories (status, starts_at, expires_at)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_official_stories_publisher  ON official_stories (publisher_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_official_stories_priority   ON official_stories (priority DESC, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_official_stories_created_at ON official_stories (created_at DESC);

-- ============================================================
-- 3. Per-viewer view tracking (only created once a view actually happens)
-- ============================================================
CREATE TABLE official_story_views (
    story_id         UUID    NOT NULL REFERENCES official_stories(id) ON DELETE CASCADE,
    viewer_user_id   UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    first_viewed_at  BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    last_viewed_at   BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    view_count       INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (story_id, viewer_user_id)
);

CREATE INDEX idx_official_story_views_viewer ON official_story_views (viewer_user_id);

-- ============================================================
-- 4. Explicit non-authentication flag for every user row
-- ============================================================
ALTER TABLE users ADD COLUMN IF NOT EXISTS can_authenticate BOOLEAN NOT NULL DEFAULT TRUE;

-- ============================================================
-- 5. Migrate the zero-UUID system account off the all-zero UUID and harden it
-- ============================================================
-- The existing official-feed-post feature (internal/admin/feed_handler.go)
-- hard-referenced users.id = 00000000-0000-0000-0000-000000000000 as its
-- author. posts.author_id is NOT NULL REFERENCES users(id) with no
-- ON UPDATE CASCADE, so the row can't be renumbered in place — insert a
-- properly hardened replacement, repoint existing posts at it, then remove
-- the old row. Application code now looks this row up by username
-- ('medhaapp'), never by a hardcoded UUID (see feed_handler.go).
-- +goose StatementBegin
DO $$
DECLARE
    new_system_user_id UUID;
    old_system_user_id CONSTANT UUID := '00000000-0000-0000-0000-000000000000';
BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE id = old_system_user_id) THEN
        -- Free the username so the new row can take it (partial unique index
        -- on username WHERE username IS NOT NULL).
        UPDATE users SET username = NULL WHERE id = old_system_user_id;

        INSERT INTO users (
            id, role, first_name, last_name, username, email, phone,
            phone_verified, profile_photo_url, profile_complete,
            auth_provider, provider_uid, is_official, can_authenticate,
            created_at, updated_at
        ) VALUES (
            uuid_generate_v4(),
            'system'::user_role,
            'Medha',
            'App',
            'medhaapp',
            NULL,
            NULL,
            FALSE,
            'https://example.com/assets/favicon.svg',
            TRUE,
            'system',
            NULL,
            TRUE,
            FALSE,
            EXTRACT(EPOCH FROM NOW())::BIGINT,
            EXTRACT(EPOCH FROM NOW())::BIGINT
        )
        RETURNING id INTO new_system_user_id;

        -- Repoint known references (posts.author_id is the only known live
        -- reference — see Phase 1 audit). If any other table still
        -- references the old id, the DELETE below fails with a foreign key
        -- violation instead of silently losing data.
        UPDATE posts SET author_id = new_system_user_id WHERE author_id = old_system_user_id;

        DELETE FROM users WHERE id = old_system_user_id;
    END IF;
END $$;
-- +goose StatementEnd

-- Belt-and-braces: no row should ever be able to authenticate with the old
-- placeholder identifiers again, even if manually re-inserted.
UPDATE users SET can_authenticate = FALSE
WHERE auth_provider = 'system' AND can_authenticate = TRUE;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS can_authenticate;
DROP TABLE IF EXISTS official_story_views;
DROP TABLE IF EXISTS official_stories;
DROP TYPE IF EXISTS official_story_status;
DROP TYPE IF EXISTS story_audience_scope;
DROP TABLE IF EXISTS system_publishers;
-- Note: this does not restore the original zero-UUID users row or revert
-- posts.author_id — see the migration report for manual restoration steps
-- if ever needed.
