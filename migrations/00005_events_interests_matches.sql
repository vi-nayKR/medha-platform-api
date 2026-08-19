-- +goose Up

CREATE TYPE ceremony_type AS ENUM (
    'shraadh', 'grihapravesh', 'vivah', 'satyanarayan', 'mundan',
    'antim_sanskar', 'vastu_shanti', 'naamkaran', 'upanayana',
    'bhagwat_katha', 'bhoomi_puja', 'chandi_homa', 'durga_puja',
    'ganesh_chaturthi', 'hanuman_jayanti', 'karthik_pournami',
    'karva_chauth', 'krishna_janmashtami', 'maha_shivaratri',
    'navratri', 'onam', 'ram_navami', 'rudra_homa',
    'akshaya_tritiya', 'annaprashan', 'dasara', 'diwali', 'engagement'
);

-- NOTE: contains legacy mixed-case duplicates — normalisation planned
CREATE TYPE event_status AS ENUM (
    'active', 'cancelled', 'completed',
    'Created', 'Active', 'Pending', 'Booked', 'Completed', 'Cancelled'
);

CREATE TYPE interest_status AS ENUM ('pending', 'accepted', 'rejected');

CREATE TYPE match_status AS ENUM ('created', 'matched', 'active', 'completed', 'cancelled');

CREATE TABLE events (
    id            UUID          PRIMARY KEY DEFAULT uuid_generate_v4(),
    yajman_id     UUID          NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ceremony_type ceremony_type NOT NULL,
    event_date    BIGINT        NOT NULL,
    location      geography(Point, 4326),
    address       TEXT          NOT NULL DEFAULT '',
    description   TEXT,
    status        event_status  NOT NULL DEFAULT 'active',
    created_at    BIGINT        NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at    BIGINT        NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    deleted_at    BIGINT
);

CREATE INDEX idx_events_yajman_id     ON events (yajman_id);
CREATE INDEX idx_events_location      ON events USING GIST (location);
CREATE INDEX idx_events_ceremony_type ON events (ceremony_type);
CREATE INDEX idx_events_status        ON events (status);
CREATE INDEX idx_events_event_date    ON events (event_date);

CREATE TRIGGER trg_events_updated_at
    BEFORE UPDATE ON events
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

CREATE TABLE interests (
    id         UUID            PRIMARY KEY DEFAULT uuid_generate_v4(),
    pandit_id  UUID            NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id   UUID            NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    message    TEXT,
    status     interest_status NOT NULL DEFAULT 'pending',
    created_at BIGINT          NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at BIGINT          NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    CONSTRAINT uq_interest_pandit_event UNIQUE (pandit_id, event_id)
);

CREATE INDEX idx_interests_pandit_id ON interests (pandit_id);
CREATE INDEX idx_interests_event_id  ON interests (event_id);
CREATE INDEX idx_interests_status    ON interests (status);

CREATE TRIGGER trg_interests_updated_at
    BEFORE UPDATE ON interests
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

CREATE TABLE matches (
    id         UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    yajman_id  UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pandit_id  UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_id   UUID         NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    status     match_status NOT NULL DEFAULT 'created',
    matched_at BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    created_at BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_matches_yajman_id ON matches (yajman_id);
CREATE INDEX idx_matches_pandit_id ON matches (pandit_id);
CREATE INDEX idx_matches_event_id  ON matches (event_id);
CREATE INDEX idx_matches_status    ON matches (status);
CREATE UNIQUE INDEX uq_match_pandit_event ON matches (pandit_id, event_id);

CREATE TRIGGER trg_matches_updated_at
    BEFORE UPDATE ON matches
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- +goose Down
DROP TABLE IF EXISTS matches;
DROP TABLE IF EXISTS interests;
DROP TABLE IF EXISTS events;
DROP TYPE IF EXISTS match_status;
DROP TYPE IF EXISTS interest_status;
DROP TYPE IF EXISTS event_status;
DROP TYPE IF EXISTS ceremony_type;
