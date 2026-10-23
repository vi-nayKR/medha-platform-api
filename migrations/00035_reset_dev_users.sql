-- +goose Up
-- Public reference snapshot: do not delete/reset named developer accounts.
-- The version is retained; demo users are not part of schema migrations.
-- See docs/PUBLICATION_REVIEW.md for existing-database limitations.
SELECT 1;

-- +goose Down
SELECT 1;
