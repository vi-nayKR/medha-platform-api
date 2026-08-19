-- +goose Up
-- +goose NO TRANSACTION
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'varamahalakshmi_vrata';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'gowri_ganesha_puja';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'ayudha_puja';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'karthika_somavara_puja';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'ganapathi_homa';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'navagraha_homa';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'sudarshana_homa';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'ayushya_homa';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'mrityunjaya_homa';
ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'seemantha';

-- Now insert/update categories in festival_logos
UPDATE festival_logos SET category = 'pitru_karya' WHERE slug IN ('shraddha', 'shraddha-path');
UPDATE festival_logos SET category = 'samskaras' WHERE slug IN ('annaprashan', 'engagement', 'namakarana', 'upanayana', 'vivah');
UPDATE festival_logos SET category = 'homas' WHERE slug IN ('chandi-homa', 'rudra-homa', 'maha-mrityunjaya-jaap');
UPDATE festival_logos SET category = 'festivals' WHERE slug IN ('akshaya-tritiya', 'dasara', 'diwali', 'durga-puja', 'ganesh-chaturthi', 'hanuman-jayanti', 'karthik-pournami', 'karva-chauth', 'krishna-janmashtami', 'maha-shivaratri', 'navratri', 'onam', 'ram-navami', 'ugadi');
UPDATE festival_logos SET category = 'pujas' WHERE slug IN ('bhoomi-puja', 'griha-pravesh', 'satyanarayan-puja', 'vastu-shanti-puja', 'sundarkand-path', 'gau-seva');

INSERT INTO festival_logos (name, slug, category, display_order, is_active) VALUES
    ('Varamahalakshmi Vrata', 'varamahalakshmi-vrata', 'festivals', 31, true),
    ('Gowri Ganesha Puja', 'gowri-ganesha-puja', 'festivals', 32, true),
    ('Ayudha Puja', 'ayudha-puja', 'festivals', 33, true),
    ('Karthika Somavara Puja', 'karthika-somavara-puja', 'festivals', 34, true),
    ('Ganapathi Homa', 'ganapathi-homa', 'homas', 35, true),
    ('Navagraha Homa', 'navagraha-homa', 'homas', 36, true),
    ('Sudarshana Homa', 'sudarshana-homa', 'homas', 37, true),
    ('Ayushya Homa', 'ayushya-homa', 'homas', 38, true),
    ('Mrityunjaya Homa', 'mrityunjaya-homa', 'homas', 39, true),
    ('Seemantha', 'seemantha', 'samskaras', 40, true)
ON CONFLICT (slug) DO UPDATE 
    SET category = EXCLUDED.category, display_order = EXCLUDED.display_order;

-- +goose Down
-- In standard PostgreSQL, removing enum values is not directly supported without rebuilding the type.
-- We can remove the seeded rows from festival_logos.
DELETE FROM festival_logos WHERE slug IN (
    'varamahalakshmi-vrata', 'gowri-ganesha-puja', 'ayudha-puja', 'karthika-somavara-puja',
    'ganapathi-homa', 'navagraha-homa', 'sudarshana-homa', 'ayushya-homa', 'mrityunjaya-homa',
    'seemantha'
);
