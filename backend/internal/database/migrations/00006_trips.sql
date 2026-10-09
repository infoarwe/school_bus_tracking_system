-- +goose Up
-- Sprint 4: daily trip assignment. Driver + Bus + Route + Date + Trip Type = one trip
-- (CLAUDE.md rule 3). Nothing here links a driver to a bus or route permanently.

CREATE TABLE trips (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid        NOT NULL REFERENCES schools (id),
    trip_date     date        NOT NULL,
    trip_type     text        NOT NULL CHECK (trip_type IN ('morning_pickup', 'evening_drop')),
    route_id      uuid        NOT NULL REFERENCES routes (id),
    bus_id        uuid        NOT NULL REFERENCES buses (id),
    driver_id     uuid        NOT NULL REFERENCES drivers (id),
    status        text        NOT NULL DEFAULT 'scheduled'
                  CHECK (status IN ('scheduled', 'confirmed', 'started', 'completed', 'cancelled')),
    notes         text        NOT NULL DEFAULT '',
    confirmed_at  timestamptz,
    started_at    timestamptz,
    ended_at      timestamptz,
    cancelled_at  timestamptz,
    cancel_reason text        NOT NULL DEFAULT '',
    created_by    uuid        REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- On one date and trip type, a route, a bus and a driver can each be in only one
-- trip. Cancelled trips do not count, so a cancelled slot can be reassigned.
CREATE UNIQUE INDEX trips_route_slot_key  ON trips (route_id,  trip_date, trip_type) WHERE status <> 'cancelled';
CREATE UNIQUE INDEX trips_bus_slot_key    ON trips (bus_id,    trip_date, trip_type) WHERE status <> 'cancelled';
CREATE UNIQUE INDEX trips_driver_slot_key ON trips (driver_id, trip_date, trip_type) WHERE status <> 'cancelled';
-- A driver can have only one trip in progress at a time.
CREATE UNIQUE INDEX trips_driver_started_key ON trips (driver_id) WHERE status = 'started';
CREATE INDEX trips_school_date_idx ON trips (school_id, trip_date);
CREATE TRIGGER trips_updated_at BEFORE UPDATE ON trips
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE trip_status_history (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id         uuid        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    school_id       uuid        NOT NULL REFERENCES schools (id),
    from_status     text,       -- null for creation
    to_status       text        NOT NULL,
    changed_by      uuid        REFERENCES users (id),
    changed_by_role text        NOT NULL DEFAULT '',
    reason          text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trip_status_history_trip_idx ON trip_status_history (trip_id, created_at);

-- +goose Down
DROP TABLE trip_status_history;
DROP TABLE trips;
