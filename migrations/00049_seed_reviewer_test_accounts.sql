-- +goose Up
INSERT INTO users (id, auth_provider, provider_uid, role, first_name, last_name, username, email, phone, phone_verified, profile_complete, location, city, state, pincode)
VALUES 
  ('e2222222-2222-2222-2222-222222222222'::uuid, 'phone', '+919999999999', 'yajman'::user_role, 'Test', 'Yajman', 'testyajman', 'test-yajman@example.com', '+919999999999', true, true, ST_SetSRID(ST_MakePoint(77.5946, 12.9716), 4326)::geography, 'Bengaluru', 'Karnataka', '560001'),
  ('e3333333-3333-3333-3333-333333333333'::uuid, 'phone', '+918888888888', 'pandit'::user_role, 'Test', 'Pandit', 'testpandit', 'test-pandit@example.com', '+918888888888', true, true, ST_SetSRID(ST_MakePoint(77.5946, 12.9716), 4326)::geography, 'Bengaluru', 'Karnataka', '560001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pandit_profiles (user_id)
VALUES
  ('e3333333-3333-3333-3333-333333333333'::uuid)
ON CONFLICT (user_id) DO NOTHING;

-- +goose Down
DELETE FROM pandit_profiles WHERE user_id = 'e3333333-3333-3333-3333-333333333333'::uuid;
DELETE FROM users WHERE id IN ('e2222222-2222-2222-2222-222222222222'::uuid, 'e3333333-3333-3333-3333-333333333333'::uuid);
