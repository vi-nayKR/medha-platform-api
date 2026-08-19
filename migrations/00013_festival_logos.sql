-- +goose Up

CREATE TABLE IF NOT EXISTS festival_logos (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL,
    slug             TEXT        NOT NULL UNIQUE,
    image_url        TEXT,
    image_url_no_bg  TEXT,
    category         VARCHAR(255),
    description      TEXT,
    is_active        BOOLEAN     NOT NULL DEFAULT true,
    display_order    INT         NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_festival_logos_slug     ON festival_logos(slug);
CREATE INDEX IF NOT EXISTS idx_festival_logos_active   ON festival_logos(is_active);
CREATE INDEX IF NOT EXISTS idx_festival_logos_order    ON festival_logos(display_order);

INSERT INTO festival_logos (name, slug, display_order) VALUES
    ('Akshaya Tritiya',       'akshaya-tritiya',       1),
    ('Annaprashan',           'annaprashan',           2),
    ('Bhagwat Katha',         'bhagwat-katha',         3),
    ('Bhoomi Puja',           'bhoomi-puja',           4),
    ('Chandi Homa',           'chandi-homa',           5),
    ('Dasara',                'dasara',                6),
    ('Diwali',                'diwali',                7),
    ('Durga Puja',            'durga-puja',            8),
    ('Engagement',            'engagement',            9),
    ('Ganesh Chaturthi',      'ganesh-chaturthi',      10),
    ('Gau Seva',              'gau-seva',              11),
    ('Griha Pravesh',         'griha-pravesh',         12),
    ('Hanuman Jayanti',       'hanuman-jayanti',       13),
    ('Karthik Pournami',      'karthik-pournami',      14),
    ('Karva Chauth',          'karva-chauth',          15),
    ('Krishna Janmashtami',   'krishna-janmashtami',   16),
    ('Maha Mrityunjaya Jaap', 'maha-mrityunjaya-jaap', 17),
    ('Maha Shivaratri',       'maha-shivaratri',       18),
    ('Namakarana',            'namakarana',            19),
    ('Navratri',              'navratri',              20),
    ('Onam',                  'onam',                  21),
    ('Ram Navami',            'ram-navami',            22),
    ('Rudra Homa',            'rudra-homa',            23),
    ('Satyanarayan Puja',     'satyanarayan-puja',     24),
    ('Shraddha',              'shraddha',              25),
    ('Sundarkand Path',       'sundarkand-path',       26),
    ('Ugadi',                 'ugadi',                 27),
    ('Upanayana',             'upanayana',             28),
    ('Vastu Shanti Puja',     'vastu-shanti-puja',     29),
    ('Vivah',                 'vivah',                 30)
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS festival_logos;
