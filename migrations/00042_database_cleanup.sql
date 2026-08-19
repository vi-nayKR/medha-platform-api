-- +goose Up
-- 1. Safely drop deprecated tables and all their dependent triggers, keys, and indexes
DROP TABLE IF EXISTS ad_campaign CASCADE;
DROP TABLE IF EXISTS ad_campaigns CASCADE;
DROP TABLE IF EXISTS admin_credentials CASCADE;
DROP TABLE IF EXISTS admin_error_logs CASCADE;
DROP TABLE IF EXISTS boosted_posts CASCADE;
DROP TABLE IF EXISTS connection_requests CASCADE;
DROP TABLE IF EXISTS conversation_participants CASCADE;
DROP TABLE IF EXISTS revenue_transactions CASCADE;
DROP TABLE IF EXISTS support_emails CASCADE;

-- 2. Clean up cities table
-- cities is being fully replaced (deleted and reseeded below with fresh rows/ids),
-- so any existing pandit_service_cities association is about to dangle regardless
-- of which city it points to — drop them first to satisfy the FK.
DELETE FROM pandit_service_cities;
DELETE FROM cities;

-- 3. Seed cities table with Karnataka-only records (to prevent duplicates, we use unique index name/state)
INSERT INTO cities (name, city, state, location, created_at, updated_at) VALUES
('Bengaluru',       'Bengaluru',       'Karnataka', ST_SetSRID(ST_MakePoint(77.5946, 12.9716), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Mysuru',          'Mysuru',          'Karnataka', ST_SetSRID(ST_MakePoint(76.6394, 12.2958), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Tumakuru',        'Tumakuru',        'Karnataka', ST_SetSRID(ST_MakePoint(77.1010, 13.3409), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Kolar',           'Kolar',           'Karnataka', ST_SetSRID(ST_MakePoint(78.1292, 13.1357), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Mandya',          'Mandya',          'Karnataka', ST_SetSRID(ST_MakePoint(76.8958, 12.5218), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Hassan',          'Hassan',          'Karnataka', ST_SetSRID(ST_MakePoint(76.1000, 13.0068), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Chikkaballapur',  'Chikkaballapur',  'Karnataka', ST_SetSRID(ST_MakePoint(77.7315, 13.4355), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Ramanagara',      'Ramanagara',      'Karnataka', ST_SetSRID(ST_MakePoint(77.2817, 12.7159), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Hubli',           'Hubli',           'Karnataka', ST_SetSRID(ST_MakePoint(75.1240, 15.3647), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Dharwad',         'Dharwad',         'Karnataka', ST_SetSRID(ST_MakePoint(75.0078, 15.4589), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Belgaum',         'Belgaum',         'Karnataka', ST_SetSRID(ST_MakePoint(74.4977, 15.8497), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Mangaluru',       'Mangaluru',       'Karnataka', ST_SetSRID(ST_MakePoint(74.8560, 12.9141), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Udupi',           'Udupi',           'Karnataka', ST_SetSRID(ST_MakePoint(74.7421, 13.3409), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Shimoga',         'Shimoga',         'Karnataka', ST_SetSRID(ST_MakePoint(75.5681, 13.9299), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Davangere',       'Davangere',       'Karnataka', ST_SetSRID(ST_MakePoint(75.9239, 14.4644), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Chitradurga',     'Chitradurga',     'Karnataka', ST_SetSRID(ST_MakePoint(76.3980, 14.2226), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Ballari',         'Ballari',         'Karnataka', ST_SetSRID(ST_MakePoint(76.9214, 15.1394), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Raichur',         'Raichur',         'Karnataka', ST_SetSRID(ST_MakePoint(77.3590, 16.2076), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Bidar',           'Bidar',           'Karnataka', ST_SetSRID(ST_MakePoint(77.5199, 17.9104), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Gulbarga',        'Gulbarga',        'Karnataka', ST_SetSRID(ST_MakePoint(76.8343, 17.3297), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Chikkamagaluru',  'Chikkamagaluru',  'Karnataka', ST_SetSRID(ST_MakePoint(75.7804, 13.3161), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Kodagu',          'Kodagu',          'Karnataka', ST_SetSRID(ST_MakePoint(75.8069, 12.3375), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Haveri',          'Haveri',          'Karnataka', ST_SetSRID(ST_MakePoint(75.3989, 14.7951), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Gadag',           'Gadag',           'Karnataka', ST_SetSRID(ST_MakePoint(75.6260, 15.4166), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Bagalkot',        'Bagalkot',        'Karnataka', ST_SetSRID(ST_MakePoint(75.6615, 16.1691), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Vijayapura',      'Vijayapura',      'Karnataka', ST_SetSRID(ST_MakePoint(75.7100, 16.8302), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Yadgir',          'Yadgir',          'Karnataka', ST_SetSRID(ST_MakePoint(77.1333, 16.7700), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Koppal',          'Koppal',          'Karnataka', ST_SetSRID(ST_MakePoint(76.1548, 15.3500), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT),
('Chamarajanagar',  'Chamarajanagar',  'Karnataka', ST_SetSRID(ST_MakePoint(76.9397, 11.9236), 4326)::geography, EXTRACT(EPOCH FROM NOW())::BIGINT, EXTRACT(EPOCH FROM NOW())::BIGINT)
ON CONFLICT (name, state) DO NOTHING;

-- +goose Down
-- Recreate structures if rolled back (not required for standard dev operation, but included for completeness)
