-- +goose Up
-- Permanent Morning/Evening route assignment: a standing driver+bus+route binding that
-- GET /driver/trips uses to auto-create today's trips row (see store.EnsureDriverDailyTrips).
-- This does NOT change the trips table or its rule-3 formula (Driver+Bus+Route+Date+Type):
-- it is only a template a daily trip is materialized from, exactly like CopyTrips already
-- does for an admin-copied day. Modeled on student_assignments: ended_at IS NULL = current.

CREATE TABLE driver_route_assignments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id  uuid        NOT NULL REFERENCES schools (id),
    route_id   uuid        NOT NULL REFERENCES routes (id),
    trip_type  text        NOT NULL CHECK (trip_type IN ('morning_pickup', 'evening_drop')),
    driver_id  uuid        NOT NULL REFERENCES drivers (id),
    bus_id     uuid        NOT NULL REFERENCES buses (id),
    created_by uuid        REFERENCES users (id),
    ended_at   timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Mirrors trips_route_slot_key / trips_bus_slot_key / trips_driver_slot_key, minus the date
-- dimension (this binding applies every day): a route's slot, a bus and a driver can each
-- hold only one current permanent assignment per trip type.
CREATE UNIQUE INDEX driver_route_assignments_route_key  ON driver_route_assignments (route_id,  trip_type) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX driver_route_assignments_bus_key    ON driver_route_assignments (bus_id,    trip_type) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX driver_route_assignments_driver_key ON driver_route_assignments (driver_id, trip_type) WHERE ended_at IS NULL;
CREATE INDEX driver_route_assignments_school_idx ON driver_route_assignments (school_id) WHERE ended_at IS NULL;
CREATE TRIGGER driver_route_assignments_updated_at BEFORE UPDATE ON driver_route_assignments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE driver_route_assignments;
