-- +goose Up
-- 1. Composite index on matches (event_id, status) to optimize event-to-match joins and lookups
CREATE INDEX IF NOT EXISTS idx_matches_event_status ON matches (event_id, status);

-- 2. Composite partial index on pandit_profiles (service_radius_km, availability_status) for availability queries
CREATE INDEX IF NOT EXISTS idx_pandit_profiles_radius_status ON pandit_profiles (service_radius_km, availability_status) WHERE deleted_at IS NULL;

-- 3. Composite partial index on events (status, event_date) to optimize event status filters and date sorts
CREATE INDEX IF NOT EXISTS idx_events_status_date ON events (status, event_date) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_events_status_date;
DROP INDEX IF EXISTS idx_pandit_profiles_radius_status;
DROP INDEX IF EXISTS idx_matches_event_status;
