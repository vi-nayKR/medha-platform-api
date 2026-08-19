-- +goose Up

-- job_leads: tracks each Pandit assignment/recommendation for an event or chat request
CREATE TABLE job_leads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        UUID REFERENCES events(id) ON DELETE CASCADE,
    conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
    pandit_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    yajman_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source          TEXT NOT NULL DEFAULT 'event',
    score           NUMERIC(5,2) NOT NULL DEFAULT 0.00,
    score_breakdown JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    platform_fee    NUMERIC(10,2) NOT NULL DEFAULT 0.00,
    fee_status      TEXT NOT NULL DEFAULT 'unpaid',
    admin_notes     TEXT NOT NULL DEFAULT '',
    assigned_by     TEXT NOT NULL DEFAULT 'auto',
    created_at      BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at      BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    deleted_at      BIGINT
);

CREATE INDEX idx_job_leads_event_id ON job_leads (event_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_job_leads_pandit_id ON job_leads (pandit_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_job_leads_yajman_id ON job_leads (yajman_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_job_leads_status ON job_leads (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_job_leads_created_at ON job_leads (created_at DESC) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_job_lead_pandit_event ON job_leads (pandit_id, event_id) WHERE deleted_at IS NULL;

CREATE TRIGGER trg_job_leads_updated_at
    BEFORE UPDATE ON job_leads
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- platform_fee_config: admin-configurable fee tiers
CREATE TABLE platform_fee_config (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ceremony_type  TEXT NOT NULL DEFAULT '*',
    fee_amount     NUMERIC(10,2) NOT NULL DEFAULT 49.00,
    fee_type       TEXT NOT NULL DEFAULT 'fixed',
    is_active      BOOLEAN NOT NULL DEFAULT true,
    created_at     BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at     BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE TRIGGER trg_platform_fee_config_updated_at
    BEFORE UPDATE ON platform_fee_config
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- Seed a default fee config
INSERT INTO platform_fee_config (ceremony_type, fee_amount, fee_type)
VALUES ('*', 49.00, 'fixed');

-- Add platform_fee field to events for event-specific fee customizations
ALTER TABLE events ADD COLUMN IF NOT EXISTS platform_fee NUMERIC(10,2);

-- +goose Down
ALTER TABLE events DROP COLUMN IF EXISTS platform_fee;
DROP TABLE IF EXISTS platform_fee_config CASCADE;
DROP TABLE IF EXISTS job_leads CASCADE;
