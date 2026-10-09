-- +goose Up
-- Sprint 5: live GPS. The current position of each bus lives in Redis; this table
-- keeps the history only for schools that retain it (CLAUDE.md: "Live location
-- history kept only where retention is enabled").

ALTER TABLE schools
    -- Days of GPS history to keep; 0 = keep none.
    ADD COLUMN location_retention_days int NOT NULL DEFAULT 30 CHECK (location_retention_days BETWEEN 0 AND 365),
    -- A started trip with no GPS for this long is shown as offline/stale.
    ADD COLUMN stale_after_seconds     int NOT NULL DEFAULT 120 CHECK (stale_after_seconds BETWEEN 30 AND 1800);

CREATE TABLE trip_locations (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id     uuid             NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    school_id   uuid             NOT NULL REFERENCES schools (id),
    latitude    double precision NOT NULL,
    longitude   double precision NOT NULL,
    accuracy_m  real             NOT NULL,
    speed_mps   real,
    heading     real,
    recorded_at timestamptz      NOT NULL, -- device time of the fix
    received_at timestamptz      NOT NULL DEFAULT now()
);
CREATE INDEX trip_locations_trip_idx ON trip_locations (trip_id, recorded_at);
CREATE INDEX trip_locations_retention_idx ON trip_locations (school_id, recorded_at);

-- +goose Down
DROP TABLE trip_locations;
ALTER TABLE schools DROP COLUMN stale_after_seconds, DROP COLUMN location_retention_days;
