-- +goose Up
-- 00035_reset_dev_users.sql seeds the dev pandit users (Koushik, Prabhanjan) into `users`
-- but never created their `pandit_profiles` row. The eligible-pandits handler and the
-- assignment engine both INNER JOIN pandit_profiles, so these seeded pandits were invisible
-- in search/assignment until they logged in once (which creates the profile elsewhere).
-- Column defaults (specializations, radius, availability) are sufficient to make them
-- show up; ON CONFLICT keeps this safe to re-run and safe if a real profile already exists.
INSERT INTO pandit_profiles (user_id)
SELECT u.id FROM users u
WHERE u.id IN (
  'c9bf29c4-ca6a-40d9-b8c5-f45cb8401217'::uuid, -- Koushik H R
  'fb0604df-3fae-4024-97e5-0b60ff124fd6'::uuid  -- Prabhanjan VK
)
ON CONFLICT (user_id) DO NOTHING;

-- +goose Down
-- Do not delete on down migration to prevent accidental data loss, just keep them.
