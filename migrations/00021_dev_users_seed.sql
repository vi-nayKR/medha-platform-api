-- +goose Up
-- Public reference snapshot: account-specific seeds are intentionally omitted.
-- Keep this migration version so existing migration ledgers remain compatible.
-- See docs/PUBLICATION_REVIEW.md; no existing database accounts are modified.
SELECT 1;

-- +goose Down
-- Never delete accounts as part of removing publication-only seed material.
SELECT 1;
