-- +goose Up
CREATE TABLE IF NOT EXISTS admin_users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username    TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL, -- bcrypt hashed password
    role        TEXT NOT NULL DEFAULT 'admin', -- 'owner' or 'admin'
    created_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at  BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX IF NOT EXISTS idx_admin_users_username ON admin_users(username);

CREATE TABLE IF NOT EXISTS event_assignment_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_id       UUID, -- optional, if relating to a lead
    event_id      UUID REFERENCES events(id) ON DELETE CASCADE,
    changed_by    TEXT NOT NULL, -- admin username or 'system' or 'auto'
    from_status   TEXT NOT NULL,
    to_status     TEXT NOT NULL,
    notes         TEXT NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX IF NOT EXISTS idx_event_assignment_logs_event_id ON event_assignment_logs(event_id);

-- +goose Down
DROP TABLE IF EXISTS event_assignment_logs CASCADE;
DROP TABLE IF EXISTS admin_users CASCADE;
