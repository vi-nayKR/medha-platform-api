-- +goose Up
-- Clean up any existing conflicting records by id, phone, or username to prevent unique constraint failures
DELETE FROM users 
WHERE id IN (
  'c9bf29c4-ca6a-40d9-b8c5-f45cb8401217'::uuid,
  'fb0604df-3fae-4024-97e5-0b60ff124fd6'::uuid,
  'c9bf29c4-ca6a-40d9-b8c5-f45cb8401219'::uuid,
  'd395cc31-37f9-438a-b348-7d2c119e07a3'::uuid
) OR phone IN (
  '+910000000002',
  '+910000000003',
  '+910000000004',
  '+910000000001'
) OR username IN (
  'koushikhr',
  'prabhanjanvk',
  'anishrao',
  'vinaykr'
);

-- Insert the dev users cleanly with explicit type casts
INSERT INTO users (id, auth_provider, provider_uid, role, first_name, last_name, username, email, phone, phone_verified, profile_complete, location, city, state, pincode, deleted_at)
VALUES 
  ('c9bf29c4-ca6a-40d9-b8c5-f45cb8401217'::uuid, 'phone', '+910000000002', 'pandit'::user_role, 'Koushik', 'H R', 'koushikhr', 'koushik.dev@example.com', '+910000000002', true, true, ST_SetSRID(ST_MakePoint(77.1181895, 13.32474), 4326)::geography, 'Tumakuru', 'Karnataka', '572102', NULL),
  ('fb0604df-3fae-4024-97e5-0b60ff124fd6'::uuid, 'phone', '+910000000003', 'pandit'::user_role, 'Prabhanjan', 'VK', 'prabhanjanvk', 'prabhanjanbk23@gmail.com', '+910000000003', true, true, ST_SetSRID(ST_MakePoint(77.1125583, 13.3348483), 4326)::geography, 'Tumakuru', 'Karnataka', '572102', NULL),
  ('c9bf29c4-ca6a-40d9-b8c5-f45cb8401219'::uuid, 'phone', '+910000000004', 'yajman'::user_role, 'Anish', 'Rao', 'anishrao', 'anishrao@gmail.com', '+910000000004', true, true, ST_SetSRID(ST_MakePoint(77.1138763, 13.3307972), 4326)::geography, 'Tumakuru', 'Karnataka', '572102', NULL),
  ('d395cc31-37f9-438a-b348-7d2c119e07a3'::uuid, 'phone', '+910000000001', 'yajman'::user_role, 'Vinay', 'KR', 'vinaykr', 'vinay.dev@example.com', '+910000000001', true, true, ST_SetSRID(ST_MakePoint(77.1181895, 13.32474), 4326)::geography, 'Tumakuru', 'Karnataka', '572102', NULL);

-- +goose Down
-- Do not delete on down migration to prevent accidental data loss in prod, just keep them.
