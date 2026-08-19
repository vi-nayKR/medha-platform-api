-- +goose Up

CREATE TYPE conversation_type AS ENUM ('pandit_pandit', 'pandit_yajman', 'help');

CREATE TYPE message_content_type AS ENUM ('text', 'image', 'location', 'system');

CREATE TABLE conversations (
    id              UUID              PRIMARY KEY DEFAULT uuid_generate_v4(),
    type            conversation_type NOT NULL,
    event_id        UUID              REFERENCES events(id) ON DELETE SET NULL,
    match_id        UUID              REFERENCES matches(id) ON DELETE SET NULL,
    title           TEXT              NOT NULL DEFAULT '',
    is_active       BOOLEAN           NOT NULL DEFAULT TRUE,
    last_message_at BIGINT,
    created_at      BIGINT            NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at      BIGINT            NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_conversations_last_msg ON conversations (last_message_at DESC);

CREATE TRIGGER trg_conversations_updated_at
    BEFORE UPDATE ON conversations
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

CREATE TABLE conversation_participants (
    id              UUID    PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID    NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id         UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_admin        BOOLEAN NOT NULL DEFAULT FALSE,
    last_read_at    BIGINT  DEFAULT 0,
    muted           BOOLEAN NOT NULL DEFAULT FALSE,
    joined_at       BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    UNIQUE (conversation_id, user_id)
);

CREATE INDEX idx_conv_participants_user ON conversation_participants (user_id);
CREATE INDEX idx_conv_participants_conv ON conversation_participants (conversation_id);

CREATE TABLE messages (
    id              UUID                 PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID                 NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       UUID                 NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content_type    message_content_type NOT NULL DEFAULT 'text',
    content         TEXT                 NOT NULL DEFAULT '',
    metadata        JSONB                DEFAULT '{}',
    created_at      BIGINT               NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_messages_conv_created ON messages (conversation_id, created_at DESC);
CREATE INDEX idx_messages_sender       ON messages (sender_id);

-- +goose Down
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversation_participants;
DROP TABLE IF EXISTS conversations;
DROP TYPE IF EXISTS message_content_type;
DROP TYPE IF EXISTS conversation_type;
