-- +goose Up

ALTER TYPE ceremony_type ADD VALUE IF NOT EXISTS 'custom';

ALTER TABLE events
    ADD COLUMN IF NOT EXISTS custom_ceremony_name TEXT,
    ADD COLUMN IF NOT EXISTS custom_ceremony_description TEXT;

COMMENT ON COLUMN events.custom_ceremony_name IS 'Yajman-provided ceremony or ritual name when ceremony_type is custom.';
COMMENT ON COLUMN events.custom_ceremony_description IS 'Yajman-provided ritual details, vidhi notes, regional customs, or samagri expectations for custom ceremonies.';

-- +goose Down

ALTER TABLE events
    DROP COLUMN IF EXISTS custom_ceremony_description,
    DROP COLUMN IF EXISTS custom_ceremony_name;
