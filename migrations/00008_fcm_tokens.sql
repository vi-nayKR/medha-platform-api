-- +goose Up
CREATE TABLE fcm_tokens (
    id         UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT        NOT NULL UNIQUE,
    device_id  TEXT        DEFAULT '',
    platform   VARCHAR(10) NOT NULL CHECK (platform IN ('android', 'ios')) DEFAULT 'android',
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at BIGINT      NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE TRIGGER trg_fcm_tokens_updated_at
    BEFORE UPDATE ON fcm_tokens
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- +goose Down
DROP TABLE IF EXISTS fcm_tokens;
