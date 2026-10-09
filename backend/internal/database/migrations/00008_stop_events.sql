-- +goose Up
-- Sprint 6: geofence stop detection and ETA.

ALTER TABLE schools
    -- The school's own location: destination of Morning Pickup ("School Reached").
    ADD COLUMN latitude            double precision CHECK (latitude BETWEEN -90 AND 90),
    ADD COLUMN longitude           double precision CHECK (longitude BETWEEN -180 AND 180),
    -- "Approaching" fires when the bus is this close to the next stop.
    ADD COLUMN approach_distance_m int NOT NULL DEFAULT 1000 CHECK (approach_distance_m BETWEEN 100 AND 5000);

-- One row per stop status change, computed from GPS (never set by hand: rule 8).
-- stop_id is NULL for the school itself (school_reached).
CREATE TABLE trip_stop_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    trip_id     uuid             NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    school_id   uuid             NOT NULL REFERENCES schools (id),
    stop_id     uuid             REFERENCES stops (id) ON DELETE SET NULL,
    event_type  text             NOT NULL CHECK (event_type IN ('approaching', 'reached', 'crossed', 'school_reached')),
    missed      boolean          NOT NULL DEFAULT false, -- crossed without ever being reached
    occurred_at timestamptz      NOT NULL,               -- time of the GPS fix that decided it
    latitude    double precision NOT NULL,
    longitude   double precision NOT NULL,
    created_at  timestamptz      NOT NULL DEFAULT now(),
    CONSTRAINT trip_stop_events_once UNIQUE NULLS NOT DISTINCT (trip_id, stop_id, event_type)
);
CREATE INDEX trip_stop_events_trip_idx ON trip_stop_events (trip_id, occurred_at);

-- +goose Down
DROP TABLE trip_stop_events;
ALTER TABLE schools DROP COLUMN approach_distance_m, DROP COLUMN longitude, DROP COLUMN latitude;
