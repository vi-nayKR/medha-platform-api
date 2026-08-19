-- +goose Up

CREATE TYPE badge_tier AS ENUM (
    'pandit_ji', 'puja_praveen', 'karma_kandi', 'yajna_maharshi', 'dharma_ratna'
);

CREATE TABLE posts (
    id            UUID    PRIMARY KEY DEFAULT uuid_generate_v4(),
    author_id     UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content       TEXT    NOT NULL DEFAULT '',
    image_urls    TEXT[]  DEFAULT '{}',
    tags          TEXT[]  DEFAULT '{}',
    like_count    INTEGER NOT NULL DEFAULT 0,
    comment_count INTEGER NOT NULL DEFAULT 0,
    is_pinned     BOOLEAN NOT NULL DEFAULT FALSE,
    post_type     TEXT    NOT NULL DEFAULT 'post',
    visibility    TEXT    NOT NULL DEFAULT 'public',
    feed_score    FLOAT8  NOT NULL DEFAULT 0,
    specialty     TEXT[]  DEFAULT '{}',
    created_at    BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    updated_at    BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    deleted_at    BIGINT
);

CREATE INDEX idx_posts_author_id  ON posts (author_id)   WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_created_at ON posts (created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_feed_score ON posts (feed_score DESC, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_posts_tags       ON posts USING GIN (tags) WHERE deleted_at IS NULL;

CREATE TRIGGER trg_posts_updated_at
    BEFORE UPDATE ON posts
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

CREATE TABLE likes (
    id         UUID   PRIMARY KEY DEFAULT uuid_generate_v4(),
    post_id    UUID   NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id    UUID   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT,
    CONSTRAINT uq_like_post_user UNIQUE (post_id, user_id)
);

CREATE INDEX idx_likes_post_id ON likes (post_id);
CREATE INDEX idx_likes_user_id ON likes (user_id);

CREATE TABLE ceremony_feedback (
    id         UUID    PRIMARY KEY DEFAULT uuid_generate_v4(),
    match_id   UUID    NOT NULL REFERENCES matches(id) ON DELETE CASCADE UNIQUE,
    yajman_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pandit_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating     INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    comment    TEXT,
    created_at BIGINT  NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_ceremony_feedback_pandit_id ON ceremony_feedback (pandit_id);
CREATE INDEX idx_ceremony_feedback_yajman_id ON ceremony_feedback (yajman_id);

CREATE TABLE pandit_badges (
    id                   UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    pandit_id            UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    badge_tier           badge_tier   NOT NULL DEFAULT 'pandit_ji',
    ceremonies_completed INTEGER      NOT NULL DEFAULT 0,
    average_rating       DECIMAL(3,2) NOT NULL DEFAULT 0,
    total_feedback_count INTEGER      NOT NULL DEFAULT 0,
    updated_at           BIGINT       NOT NULL DEFAULT (EXTRACT(EPOCH FROM NOW()))::BIGINT
);

CREATE INDEX idx_pandit_badges_pandit_id ON pandit_badges (pandit_id);

CREATE TRIGGER trg_pandit_badges_updated_at
    BEFORE UPDATE ON pandit_badges
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_timestamp();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION create_pandit_badge()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO pandit_badges (pandit_id) VALUES (NEW.user_id)
    ON CONFLICT (pandit_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_auto_create_pandit_badge
    AFTER INSERT ON pandit_profiles
    FOR EACH ROW
    EXECUTE FUNCTION create_pandit_badge();

-- +goose Down
DROP TRIGGER IF EXISTS trg_auto_create_pandit_badge ON pandit_profiles;
DROP FUNCTION IF EXISTS create_pandit_badge();
DROP TABLE IF EXISTS pandit_badges;
DROP TABLE IF EXISTS ceremony_feedback;
DROP TABLE IF EXISTS likes;
DROP TABLE IF EXISTS posts;
DROP TYPE IF EXISTS badge_tier;
