-- +goose Up
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS postgis;

-- +goose StatementBegin
DO $$
BEGIN
    -- Ensure spatial_ref_sys is populated if it exists but is missing standard SRID 4326
    IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'spatial_ref_sys') THEN
        IF NOT EXISTS (SELECT 1 FROM spatial_ref_sys WHERE srid = 4326) THEN
            PERFORM populate_spatial_ref_sys();
        END IF;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP EXTENSION IF EXISTS postgis;
DROP EXTENSION IF EXISTS pgcrypto;
DROP EXTENSION IF EXISTS "uuid-ossp";
