-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trigger_set_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TYPE user_role AS ENUM ('yajman', 'pandit');

CREATE TABLE users (
    id                    UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    firebase_uid          TEXT         UNIQUE,
    role                  user_role,
    first_name            TEXT         NOT NULL DEFAULT '',
    last_name             TEXT         NOT NULL DEFAULT '',
    username              TEXT,
    email                 TEXT,
    phone                 VARCHAR(20)  UNIQUE,
    phone_verified        BOOLEAN      NOT NULL DEFAULT FALSE,
    profile_photo_url     TEXT,
    profile_complete      BOOLEAN      NOT NULL DEFAULT FALSE,
    location              geography(Point, 4326),
    firebase_refresh_token TEXT,
    auth_provider         TEXT         NOT NULL DEFAULT 'google',
    provider_uid          TEXT,
    google_id             VARCHAR(255) UNIQUE,
    apple_id              VARCHAR(255) UNIQUE,
    city                  VARCHAR(100),
    state                 VARCHAR(100),
    pincode               VARCHAR(20),
    created_at            BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at            BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    deleted_at            BIGINT,
    CONSTRAINT uq_users_provider_uid UNIQUE (auth_provider, provider_uid)
);

CREATE INDEX idx_users_firebase_uid    ON users (firebase_uid);
CREATE INDEX idx_users_role            ON users (role);
CREATE INDEX idx_users_location        ON users USING GIST (location);
CREATE UNIQUE INDEX idx_users_username ON users (username) WHERE username IS NOT NULL;
CREATE INDEX idx_users_phone           ON users (phone);
CREATE INDEX idx_users_auth_provider   ON users (auth_provider, provider_uid);
CREATE INDEX idx_users_google_id       ON users (google_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_users_apple_id        ON users (apple_id)  WHERE deleted_at IS NULL;
CREATE INDEX idx_users_city_pandit     ON users (city) WHERE role = 'pandit' AND deleted_at IS NULL;

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

CREATE TABLE refresh_tokens (
    id         UUID   PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT   NOT NULL UNIQUE,
    expires_at BIGINT NOT NULL,
    revoked_at BIGINT,
    created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens (user_id);

-- +goose Down
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS user_role;
DROP FUNCTION IF EXISTS trigger_set_timestamp();
