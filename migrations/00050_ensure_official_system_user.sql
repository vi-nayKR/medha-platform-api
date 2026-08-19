-- +goose Up
-- Ensure the official Medha App system user exists in the users table.
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
    can_authenticate,
    created_at,
    updated_at
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
    'medha-app-system-user',
    TRUE,
    FALSE,
    EXTRACT(EPOCH FROM NOW())::BIGINT,
    EXTRACT(EPOCH FROM NOW())::BIGINT
) ON CONFLICT (username) WHERE username IS NOT NULL DO UPDATE SET
    role = 'system'::user_role,
    first_name = 'Medha',
    last_name = 'App',
    profile_photo_url = 'https://example.com/assets/favicon.svg',
    is_official = TRUE,
    can_authenticate = FALSE,
    deleted_at = NULL;

-- Ensure system_publishers row exists as well
INSERT INTO system_publishers (publisher_key, display_name, username, avatar_url, is_verified, is_active)
VALUES ('medha-app', 'Medha', 'medhaapp', 'https://example.com/assets/favicon.svg', TRUE, TRUE)
ON CONFLICT (publisher_key) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    username = EXCLUDED.username,
    avatar_url = EXCLUDED.avatar_url,
    is_active = TRUE;

-- +goose Down
-- Do not delete system user on rollback to prevent breaking foreign keys.
