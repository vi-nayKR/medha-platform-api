-- +goose Up
-- Public reference snapshot: no reviewer identities or privileged login seeds.
-- Normal OTP authentication must always use the configured provider.
-- Existing installations require an owner-reviewed account/session audit;
-- editing an already-applied migration does not revoke accounts or tokens.
SELECT 1;

-- +goose Down
SELECT 1;
