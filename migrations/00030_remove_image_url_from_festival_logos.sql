-- +goose Up
ALTER TABLE festival_logos DROP COLUMN IF EXISTS image_url;

-- +goose Down
ALTER TABLE festival_logos ADD COLUMN image_url TEXT;
