DROP INDEX IF EXISTS restaurants_cuisines_gin_idx;
DROP INDEX IF EXISTS restaurants_approved_open_idx;
DROP INDEX IF EXISTS restaurants_location_gist_idx;
ALTER TABLE restaurants
    DROP COLUMN IF EXISTS location,
    DROP COLUMN IF EXISTS cuisines,
    DROP COLUMN IF EXISTS approval_status;
