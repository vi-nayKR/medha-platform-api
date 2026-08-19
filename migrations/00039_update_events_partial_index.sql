-- +goose Up
DROP INDEX IF EXISTS idx_events_status_partial;
CREATE INDEX idx_events_status_partial ON events (status) 
WHERE (deleted_at IS NULL AND status = ANY (ARRAY['Active'::event_status, 'Pending'::event_status, 'Pushed'::event_status]));

-- +goose Down
DROP INDEX IF EXISTS idx_events_status_partial;
CREATE INDEX idx_events_status_partial ON events (status) 
WHERE (deleted_at IS NULL AND status = ANY (ARRAY['Created'::event_status, 'Active'::event_status, 'Pending'::event_status]));
