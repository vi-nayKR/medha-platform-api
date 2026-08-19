-- +goose Up
-- The official Medha App system user (id 00000000-0000-0000-0000-000000000000,
-- originally created by migration 00041) was found missing on dev: something
-- deleted it directly from the users table after migration 41 ran, so goose
-- never re-applies that migration to recreate it. Every /api/v2/admin/feed post
-- has author_id set to this row via a NOT NULL foreign key, so its absence made
-- publishing fail with a foreign key violation. Re-inserting it here,
-- idempotently, so a future accidental delete can be fixed the same way.
INSERT INTO users (
    id,
    role,
    first_name,
    last_name,
    username,
    email,
    phone,
    phone_verified,
    profile_photo_url,
    profile_complete,
    auth_provider,
    provider_uid,
    is_official,
    created_at,
    updated_at
) VALUES (
    '00000000-0000-0000-0000-000000000000'::uuid,
    'yajman'::user_role,
    'Medha',
    'App',
    'medhaapp',
    'app@example.com',
    '+910000000000',
    TRUE,
    'https://example.com/assets/favicon.svg',
    TRUE,
    'system',
    'medha-app-system-user',
    TRUE,
    EXTRACT(EPOCH FROM NOW())::BIGINT,
    EXTRACT(EPOCH FROM NOW())::BIGINT
) ON CONFLICT (id) DO UPDATE SET
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    username = EXCLUDED.username,
    email = EXCLUDED.email,
    profile_photo_url = EXCLUDED.profile_photo_url,
    is_official = TRUE,
    deleted_at = NULL;

-- +goose Down
-- Intentionally a no-op: this migration only restores data that migration
-- 00041 already owns deleting on its down migration.
SELECT 1;
