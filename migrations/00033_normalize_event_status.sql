-- +goose Up
-- 1. Drop dependent indexes referencing status column
DROP INDEX IF EXISTS idx_events_status;
DROP INDEX IF EXISTS idx_events_location_partial;
DROP INDEX IF EXISTS idx_events_status_partial;
DROP INDEX IF EXISTS idx_events_status_date;

-- 2. Normalise existing legacy lowercase event statuses to capitalized statuses
UPDATE events SET status = 'Active' WHERE status::text = 'active';
UPDATE events SET status = 'Cancelled' WHERE status::text = 'cancelled';
UPDATE events SET status = 'Completed' WHERE status::text = 'completed';

-- 3. Drop column default to allow modification of column type
ALTER TABLE events ALTER COLUMN status DROP DEFAULT;

-- 4. Rename existing enum
ALTER TYPE event_status RENAME TO event_status_old;

-- 5. Create new normalized enum
CREATE TYPE event_status AS ENUM ('Created', 'Active', 'Pending', 'Booked', 'Completed', 'Cancelled');

-- 6. Cast status column to new enum type
ALTER TABLE events ALTER COLUMN status TYPE event_status USING status::text::event_status;

-- 7. Restore default value to capitalized 'Active'
ALTER TABLE events ALTER COLUMN status SET DEFAULT 'Active';

-- 8. Drop legacy enum
DROP TYPE event_status_old;

-- 9. Recreate the dropped indexes
CREATE INDEX idx_events_status ON events (status);
CREATE INDEX idx_events_location_partial ON events USING GIST (location) 
  WHERE (deleted_at IS NULL AND status = ANY (ARRAY['Created'::event_status, 'Active'::event_status, 'Pending'::event_status]));
CREATE INDEX idx_events_status_partial ON events (status) WHERE (deleted_at IS NULL);
CREATE INDEX idx_events_status_date ON events (status, event_date) WHERE (deleted_at IS NULL);

-- +goose Down
DROP INDEX IF EXISTS idx_events_status;
DROP INDEX IF EXISTS idx_events_location_partial;
DROP INDEX IF EXISTS idx_events_status_partial;
DROP INDEX IF EXISTS idx_events_status_date;

ALTER TYPE event_status RENAME TO event_status_new;
CREATE TYPE event_status AS ENUM ('active', 'cancelled', 'completed', 'Created', 'Active', 'Pending', 'Booked', 'Completed', 'Cancelled');
ALTER TABLE events ALTER COLUMN status DROP DEFAULT;
ALTER TABLE events ALTER COLUMN status TYPE event_status USING status::text::event_status;
ALTER TABLE events ALTER COLUMN status SET DEFAULT 'active';
DROP TYPE event_status_new;

CREATE INDEX idx_events_status ON events (status);
CREATE INDEX idx_events_location_partial ON events USING GIST (location) 
  WHERE (deleted_at IS NULL AND status = ANY (ARRAY['Created'::event_status, 'Active'::event_status, 'Pending'::event_status]));
CREATE INDEX idx_events_status_partial ON events (status) WHERE (deleted_at IS NULL);
CREATE INDEX idx_events_status_date ON events (status, event_date) WHERE (deleted_at IS NULL);
