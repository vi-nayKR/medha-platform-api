-- +goose Up

CREATE TYPE availability_status AS ENUM ('available', 'unavailable');

CREATE TABLE pandit_profiles (
    id                       UUID                PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id                  UUID                NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    parampara                TEXT                NOT NULL DEFAULT '',
    veda_affiliation         TEXT                NOT NULL DEFAULT '',
    ceremony_specializations TEXT[]              NOT NULL DEFAULT '{}',
    languages                TEXT[]              NOT NULL DEFAULT '{}',
    service_radius_km        INTEGER             NOT NULL DEFAULT 25,
    availability_status      availability_status NOT NULL DEFAULT 'available',
    about                    TEXT,
    created_at               BIGINT              NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at               BIGINT              NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    deleted_at               BIGINT
);

CREATE INDEX idx_pandit_profiles_availability_status      ON pandit_profiles (availability_status);
CREATE INDEX idx_pandit_profiles_ceremony_specializations ON pandit_profiles USING GIN (ceremony_specializations);

CREATE TRIGGER trg_pandit_profiles_updated_at
    BEFORE UPDATE ON pandit_profiles
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- +goose Down
DROP TABLE IF EXISTS pandit_profiles;
DROP TYPE IF EXISTS availability_status;
