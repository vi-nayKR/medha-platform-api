-- 00012_cleanup_tables.sql
-- Database cleanup and updates:
-- 1. Renames/Migrates ceremony_feedback -> event_feedback
-- 2. Adds description to festivals table and populates it from ceremony_catalog
-- 3. Drops ceremony_catalog (which is replaced by festival_logos)
-- 4. Replaces admin_request_logs with admin_error_logs

-- +goose Up
-- 1. Create event_feedback table
CREATE TABLE IF NOT EXISTS event_feedback (
    id         UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id   UUID    NOT NULL REFERENCES matches(id) ON DELETE CASCADE UNIQUE,
    yajman_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pandit_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating     INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    comment    TEXT,
    created_at BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX IF NOT EXISTS idx_event_feedback_pandit_id ON event_feedback (pandit_id);
CREATE INDEX IF NOT EXISTS idx_event_feedback_yajman_id ON event_feedback (yajman_id);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_tables WHERE schemaname = 'public' AND tablename = 'ceremony_feedback') THEN
        INSERT INTO event_feedback (id, match_id, yajman_id, pandit_id, rating, comment, created_at)
        SELECT id, match_id, yajman_id, pandit_id, rating, comment, created_at
        FROM ceremony_feedback
        ON CONFLICT (match_id) DO NOTHING;
    END IF;
END $$;
-- +goose StatementEnd

-- 3. Drop ceremony_feedback
DROP TABLE IF EXISTS ceremony_feedback CASCADE;

-- 4. Add description to festivals table
ALTER TABLE festivals ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_tables WHERE schemaname = 'public' AND tablename = 'ceremony_catalog') THEN
        UPDATE festivals f
        SET description = c.description
        FROM ceremony_catalog c
        WHERE LOWER(TRIM(f.festival)) = LOWER(TRIM(c.display_name))
          AND c.description IS NOT NULL
          AND c.description <> '';
    END IF;
END $$;
-- +goose StatementEnd

-- 6. Drop ceremony_catalog table
DROP TABLE IF EXISTS ceremony_catalog CASCADE;

-- +goose Down
-- Recreate ceremony_catalog
CREATE TABLE IF NOT EXISTS ceremony_catalog (
    slug         TEXT   PRIMARY KEY,
    display_name TEXT   NOT NULL,
    logo_url     TEXT   NOT NULL,
    category     TEXT   NOT NULL DEFAULT 'general',
    description  TEXT,
    created_at   BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

-- Remove description from festivals
ALTER TABLE festivals DROP COLUMN IF EXISTS description;

-- Recreate ceremony_feedback
CREATE TABLE IF NOT EXISTS ceremony_feedback (
    id         UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id   UUID    NOT NULL REFERENCES matches(id) ON DELETE CASCADE UNIQUE,
    yajman_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pandit_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating     INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    comment    TEXT,
    created_at BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);
CREATE INDEX IF NOT EXISTS idx_ceremony_feedback_pandit_id ON ceremony_feedback (pandit_id);
CREATE INDEX IF NOT EXISTS idx_ceremony_feedback_yajman_id ON ceremony_feedback (yajman_id);

-- Migrate data back
INSERT INTO ceremony_feedback (id, match_id, yajman_id, pandit_id, rating, comment, created_at)
SELECT id, match_id, yajman_id, pandit_id, rating, comment, created_at
FROM event_feedback
ON CONFLICT (match_id) DO NOTHING;

-- Drop event_feedback
DROP TABLE IF EXISTS event_feedback CASCADE;
