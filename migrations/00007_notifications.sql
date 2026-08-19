-- +goose Up

CREATE TYPE notification_type AS ENUM (
    'event_nearby', 'interest_received', 'match_confirmed', 'social_like',
    'event_expiring', 'badge_earned', 'match_completed', 'interest_accepted',
    'connection_request', 'connection_confirmed', 'chat_message',
    'booking_confirmed', 'timeline_risk'
);

CREATE TABLE notifications (
    id           UUID              PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id      UUID              NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type         notification_type NOT NULL,
    title        TEXT              NOT NULL,
    body         TEXT              NOT NULL,
    data         JSONB             DEFAULT '{}',
    read         BOOLEAN           NOT NULL DEFAULT FALSE,
    delivered_at BIGINT,
    created_at   BIGINT            NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_notifications_user_id    ON notifications (user_id);
CREATE INDEX idx_notifications_created_at ON notifications (created_at DESC);
CREATE INDEX idx_notifications_read       ON notifications (user_id, read) WHERE read = FALSE;

CREATE TABLE notification_log (
    id       UUID   PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID   NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    tier     TEXT   NOT NULL CHECK (tier IN ('low', 'medium', 'high')),
    sent_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    CONSTRAINT uq_notification_log_event_tier UNIQUE (event_id, tier)
);

CREATE INDEX idx_notification_log_event_id ON notification_log (event_id);

-- +goose Down
DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS notifications;
DROP TYPE IF EXISTS notification_type;
