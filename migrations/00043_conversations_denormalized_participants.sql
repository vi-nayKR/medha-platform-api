-- +goose Up
-- Migration 00042 dropped conversation_participants before conversations had
-- user_one_id/user_two_id to replace it with (that change had been made by editing
-- migration 00011 in place, which is a no-op on any DB that already ran version 11).
-- This migration adds the columns for real, via ALTER TABLE, so it applies regardless
-- of when a given environment first ran migration 00011.
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS user_one_id UUID REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS user_two_id UUID REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS user_one_last_read_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS user_two_last_read_at BIGINT NOT NULL DEFAULT 0;

-- Backfill from conversation_participants where that table still exists (environments
-- where 00042 hasn't run yet). is_admin participants map to user_one_id to match the
-- application's existing IsAdmin=true-for-user-one convention.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('public.conversation_participants') IS NOT NULL THEN
        UPDATE conversations c SET
            user_one_id = ranked.user_one_id,
            user_two_id = ranked.user_two_id,
            user_one_last_read_at = COALESCE(ranked.user_one_read_at, 0),
            user_two_last_read_at = COALESCE(ranked.user_two_read_at, 0)
        FROM (
            SELECT
                conversation_id,
                (ARRAY_AGG(user_id ORDER BY is_admin DESC, joined_at ASC))[1] AS user_one_id,
                (ARRAY_AGG(user_id ORDER BY is_admin DESC, joined_at ASC))[2] AS user_two_id,
                (ARRAY_AGG(last_read_at ORDER BY is_admin DESC, joined_at ASC))[1] AS user_one_read_at,
                (ARRAY_AGG(last_read_at ORDER BY is_admin DESC, joined_at ASC))[2] AS user_two_read_at
            FROM conversation_participants
            GROUP BY conversation_id
        ) ranked
        WHERE c.id = ranked.conversation_id;
    END IF;
END $$;
-- +goose StatementEnd

-- Any conversation still without a user_one_id has no recoverable participant data
-- (conversation_participants was already dropped by 00042 before this ran). It's
-- unusable — no way to know who was in it — so remove it; messages cascade-delete.
DELETE FROM conversations WHERE user_one_id IS NULL;

ALTER TABLE conversations ALTER COLUMN user_one_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_conversations_user_one ON conversations (user_one_id);
CREATE INDEX IF NOT EXISTS idx_conversations_user_two ON conversations (user_two_id);

-- +goose Down
DROP INDEX IF EXISTS idx_conversations_user_two;
DROP INDEX IF EXISTS idx_conversations_user_one;
ALTER TABLE conversations DROP COLUMN IF EXISTS user_two_last_read_at;
ALTER TABLE conversations DROP COLUMN IF EXISTS user_one_last_read_at;
ALTER TABLE conversations DROP COLUMN IF EXISTS user_two_id;
ALTER TABLE conversations DROP COLUMN IF EXISTS user_one_id;
