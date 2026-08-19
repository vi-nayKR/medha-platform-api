-- +goose Up
-- 1. Create a new enum type with the new values added
CREATE TYPE interest_status_new AS ENUM (
    'pending',
    'accepted',
    'rejected',
    'connected',
    'withdrawn',
    'event_cancelled',
    'completed'
);

-- 2. Drop the default values on interests.status
ALTER TABLE interests ALTER COLUMN status DROP DEFAULT;

-- 3. Change column types using the new type
ALTER TABLE interests ALTER COLUMN status TYPE interest_status_new USING status::text::interest_status_new;

-- 4. Drop the old enum type
DROP TYPE interest_status;

-- 5. Rename the new enum type to the original name
ALTER TYPE interest_status_new RENAME TO interest_status;

-- 6. Set the defaults back
ALTER TABLE interests ALTER COLUMN status SET DEFAULT 'pending'::interest_status;


-- +goose Down
-- 1. Recreate the old type with original 3 values
CREATE TYPE interest_status_old AS ENUM ('pending', 'accepted', 'rejected');

-- 2. Map the new enum values back to the original values to prevent conversion failures
UPDATE interests SET status = 'pending' WHERE status = 'connected';
UPDATE interests SET status = 'rejected' WHERE status = 'withdrawn';
UPDATE interests SET status = 'rejected' WHERE status = 'event_cancelled';
UPDATE interests SET status = 'accepted' WHERE status = 'completed';

-- 3. Drop the default values on interests.status
ALTER TABLE interests ALTER COLUMN status DROP DEFAULT;

-- 4. Change column types to interest_status_old
ALTER TABLE interests ALTER COLUMN status TYPE interest_status_old USING status::text::interest_status_old;

-- 5. Drop the new enum type
DROP TYPE interest_status;

-- 6. Rename the old enum type back to the original name
ALTER TYPE interest_status_old RENAME TO interest_status;

-- 7. Set the defaults back
ALTER TABLE interests ALTER COLUMN status SET DEFAULT 'pending'::interest_status;
