-- +goose Up
-- Add is_official column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_official BOOLEAN NOT NULL DEFAULT FALSE;

-- Create official system user for Medha App
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
    is_official = TRUE;

-- +goose Down
DELETE FROM users WHERE id = '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE users DROP COLUMN IF EXISTS is_official;
