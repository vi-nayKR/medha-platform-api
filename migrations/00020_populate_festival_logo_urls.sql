-- +goose Up
UPDATE festival_logos
SET image_url = 'public/festival-logos/' || slug || '.png',
    image_url_no_bg = 'public/festival-logos/' || slug || '-no-bg.png'
WHERE image_url IS NULL OR image_url = '';

-- +goose Down
UPDATE festival_logos
SET image_url = NULL,
    image_url_no_bg = NULL
WHERE image_url LIKE 'public/festival-logos/%' AND image_url_no_bg LIKE 'public/festival-logos/%';
