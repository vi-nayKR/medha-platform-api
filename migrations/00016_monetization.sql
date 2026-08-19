-- 00013_monetization.sql
-- Database updates for Monetization, Reports, and Analytics:
-- 1. Adds premium subscription fields to users table
-- 2. Creates ad_campaigns table for ad display & metrics management
-- 3. Creates revenue_transactions table for financial reporting in INR
-- 4. Creates boosted_posts table for post boost campaigns

-- +goose Up
-- 1. Add premium fields to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_premium BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS premium_until BIGINT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS premium_badge TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS premium_features JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_users_is_premium ON users (is_premium) WHERE deleted_at IS NULL;

-- +goose Down
-- Remove premium fields from users table
ALTER TABLE users DROP COLUMN IF EXISTS premium_features;
ALTER TABLE users DROP COLUMN IF EXISTS premium_badge;
ALTER TABLE users DROP COLUMN IF EXISTS premium_until;
ALTER TABLE users DROP COLUMN IF EXISTS is_premium;
