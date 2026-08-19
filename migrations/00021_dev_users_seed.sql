-- +goose Up
-- Seed the 4 allowed developer users with exact profiles
INSERT INTO users (id, auth_provider, provider_uid, role, first_name, last_name, username, email, phone, phone_verified, profile_complete, location, city, state, pincode)
VALUES 
  ('c9bf29c4-ca6a-40d9-b8c5-f45cb8401217', 'phone', '+910000000002', 'pandit', 'Koushik', 'H R', 'koushikhr', 'koushik.dev@example.com', '+910000000002', true, true, ST_SetSRID(ST_MakePoint(77.1181895, 13.32474), 4326)::geography, 'Tumakuru', 'Karnataka', '572102'),
  ('fb0604df-3fae-4024-97e5-0b60ff124fd6', 'phone', '+910000000003', 'pandit', 'Prabhanjan', 'VK', 'prabhanjanvk', 'prabhanjanbk23@gmail.com', '+910000000003', true, true, ST_SetSRID(ST_MakePoint(77.1125583, 13.3348483), 4326)::geography, 'Tumakuru', 'Karnataka', '572102'),
  ('c9bf29c4-ca6a-40d9-b8c5-f45cb8401219', 'phone', '+910000000004', 'yajman', 'Anish', 'Rao', 'anishrao', 'anishrao@gmail.com', '+910000000004', true, true, ST_SetSRID(ST_MakePoint(77.1138763, 13.3307972), 4326)::geography, 'Tumakuru', 'Karnataka', '572102'),
  ('d395cc31-37f9-438a-b348-7d2c119e07a3', 'phone', '+910000000001', 'yajman', 'Vinay', 'KR', 'vinaykr', 'vinay.dev@example.com', '+910000000001', true, true, ST_SetSRID(ST_MakePoint(77.1181895, 13.32474), 4326)::geography, 'Tumakuru', 'Karnataka', '572102')
ON CONFLICT (phone) DO UPDATE 
SET auth_provider = EXCLUDED.auth_provider,
    provider_uid = EXCLUDED.provider_uid,
    role = EXCLUDED.role,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    username = EXCLUDED.username,
    email = EXCLUDED.email,
    phone_verified = EXCLUDED.phone_verified,
    profile_complete = EXCLUDED.profile_complete,
    location = EXCLUDED.location,
    city = EXCLUDED.city,
    state = EXCLUDED.state,
    pincode = EXCLUDED.pincode;

-- +goose Down
DELETE FROM users WHERE phone IN ('+910000000002', '+910000000003', '+910000000004', '+910000000001');
