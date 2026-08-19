-- +goose Up
-- 1. Partial index on users(email) for fast logins and lookups
CREATE INDEX IF NOT EXISTS idx_users_email_partial 
    ON users (email) 
    WHERE deleted_at IS NULL;

-- 2. Composite partial index on events for yajman list query cursor pagination
CREATE INDEX IF NOT EXISTS idx_events_yajman_date_id 
    ON events (yajman_id, event_date ASC, id ASC) 
    WHERE deleted_at IS NULL;

-- 3. Spatial partial index on active events
CREATE INDEX IF NOT EXISTS idx_events_location_partial 
    ON events USING GIST (location) 
    WHERE deleted_at IS NULL AND status IN ('Created', 'Active', 'Pending');

-- 4. Partial index on events status
CREATE INDEX IF NOT EXISTS idx_events_status_partial 
    ON events (status) 
    WHERE deleted_at IS NULL;

-- 5. Composite index on notifications for user pagination
CREATE INDEX IF NOT EXISTS idx_notifications_user_created_id 
    ON notifications (user_id, created_at DESC, id DESC);

-- 6. Composite index on messages for conversation scroll pagination
CREATE INDEX IF NOT EXISTS idx_messages_conv_created_id 
    ON messages (conversation_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_messages_conv_created_id;
DROP INDEX IF EXISTS idx_notifications_user_created_id;
DROP INDEX IF EXISTS idx_events_status_partial;
DROP INDEX IF EXISTS idx_events_location_partial;
DROP INDEX IF EXISTS idx_events_yajman_date_id;
DROP INDEX IF EXISTS idx_users_email_partial;
