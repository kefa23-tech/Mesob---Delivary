CREATE EXTENSION IF NOT EXISTS postgis;

ALTER TABLE restaurants
    ADD COLUMN approval_status TEXT NOT NULL DEFAULT 'pending' CHECK (approval_status IN ('pending', 'approved', 'rejected', 'suspended')),
    ADD COLUMN cuisines TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN location GEOGRAPHY(Point, 4326)
        GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(lng, lat), 4326)::GEOGRAPHY) STORED;

CREATE INDEX restaurants_location_gist_idx ON restaurants USING GIST (location);
CREATE INDEX restaurants_approved_open_idx ON restaurants (approval_status, is_open);
CREATE INDEX restaurants_cuisines_gin_idx ON restaurants USING GIN (cuisines);
