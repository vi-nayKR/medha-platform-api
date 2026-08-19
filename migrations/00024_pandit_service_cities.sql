-- +goose Up
CREATE TABLE pandit_service_cities (
    id          UUID    PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    city_id     UUID    NOT NULL REFERENCES cities(id) ON DELETE RESTRICT,
    is_home     BOOLEAN NOT NULL DEFAULT false,
    created_at  BIGINT  NOT NULL
);

-- "Which cities does this pandit serve?"
CREATE INDEX idx_psc_user ON pandit_service_cities(user_id);

-- "Which pandits serve this city?" (yajman discovery)
CREATE INDEX idx_psc_city ON pandit_service_cities(city_id);

-- Only one home city per pandit
CREATE UNIQUE INDEX idx_psc_home_unique
    ON pandit_service_cities(user_id)
    WHERE is_home = true;

-- No duplicate city per pandit
CREATE UNIQUE INDEX idx_psc_user_city
    ON pandit_service_cities(user_id, city_id);

-- +goose Down
DROP TABLE IF EXISTS pandit_service_cities;
