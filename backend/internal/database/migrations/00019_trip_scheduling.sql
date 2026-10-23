-- +goose Up
-- Multiple scheduled trips per bus/driver/day: a bus or driver can now run any
-- number of trips in a day, including the same route twice, as long as their
-- scheduled windows don't overlap. Replaces the old "one trip per (x, date,
-- trip_type)" rule with a real time-range overlap check.

-- Nullable on purpose: existing trips have no recorded time-of-day (only a
-- calendar trip_date), and this repo's own data has routes with no stop times
-- at all (special/template routes) — there is no reliable way to invent a
-- time for those, so they keep NULL rather than a guessed value. The
-- exclusion constraints below only apply where both times are actually set,
-- so historical rows are completely unaffected either way.
ALTER TABLE trips ADD COLUMN scheduled_start_at timestamptz;
ALTER TABLE trips ADD COLUMN scheduled_end_at timestamptz;
ALTER TABLE trips ADD CONSTRAINT trips_scheduled_window_order
    CHECK (scheduled_start_at IS NULL OR scheduled_end_at IS NULL OR scheduled_end_at > scheduled_start_at);

-- Best-effort backfill from the route's own stop times, only where they
-- exist: morning_pickup uses each stop's pickup_time (first stop = start,
-- last = end + 15 min as a nominal arrival buffer); evening_drop uses
-- drop_time the same way. A route with no stops carrying that time, or a
-- routeless special_trip, is left NULL — never guessed.
-- Computed as an independent subquery (joining trips as an ordinary, non-target
-- table) rather than a LATERAL correlated directly against the UPDATE's own
-- target table t — Postgres does not allow a FROM-clause LATERAL item to
-- correlate against an UPDATE statement's target in this shape.
UPDATE trips SET
    scheduled_start_at = sub.start_at,
    scheduled_end_at   = sub.end_at
FROM (
    SELECT t.id,
        (t.trip_date + agg.min_t) AT TIME ZONE s.timezone AS start_at,
        (t.trip_date + agg.max_t + interval '15 minutes') AT TIME ZONE s.timezone AS end_at
    FROM trips t
    JOIN schools s ON s.id = t.school_id
    JOIN LATERAL (
        SELECT min(CASE WHEN t.trip_type = 'morning_pickup' THEN st.pickup_time ELSE st.drop_time END) AS min_t,
            max(CASE WHEN t.trip_type = 'morning_pickup' THEN st.pickup_time ELSE st.drop_time END) AS max_t
        FROM stops st WHERE st.route_id = t.route_id
    ) agg ON true
    WHERE t.route_id IS NOT NULL AND t.trip_type IN ('morning_pickup', 'evening_drop')
        AND agg.min_t IS NOT NULL AND agg.max_t IS NOT NULL
) sub
WHERE trips.id = sub.id;

-- btree_gist lets a GiST exclusion constraint also compare the plain uuid
-- columns (bus_id/driver_id/route_id) for equality alongside the time-range
-- overlap check.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Replaces trips_route_slot_key / trips_bus_slot_key / trips_driver_slot_key:
-- same three dimensions, but keyed on actual overlapping time windows instead
-- of a flat (date, trip_type) tuple, so any number of non-overlapping trips
-- can now share a route, bus or driver on the same day. A trip with no
-- scheduled window (NULL) is exempt from this check entirely (see above) —
-- it falls back to no automatic overlap protection, same as before this
-- migration, until it's given a real time.
DROP INDEX trips_route_slot_key;
DROP INDEX trips_bus_slot_key;
DROP INDEX trips_driver_slot_key;

ALTER TABLE trips ADD CONSTRAINT trips_route_no_overlap
    EXCLUDE USING gist (route_id WITH =, tstzrange(scheduled_start_at, scheduled_end_at, '[)') WITH &&)
    WHERE (status <> 'cancelled' AND scheduled_start_at IS NOT NULL AND scheduled_end_at IS NOT NULL);
ALTER TABLE trips ADD CONSTRAINT trips_bus_no_overlap
    EXCLUDE USING gist (bus_id WITH =, tstzrange(scheduled_start_at, scheduled_end_at, '[)') WITH &&)
    WHERE (status <> 'cancelled' AND scheduled_start_at IS NOT NULL AND scheduled_end_at IS NOT NULL);
ALTER TABLE trips ADD CONSTRAINT trips_driver_no_overlap
    EXCLUDE USING gist (driver_id WITH =, tstzrange(scheduled_start_at, scheduled_end_at, '[)') WITH &&)
    WHERE (status <> 'cancelled' AND scheduled_start_at IS NOT NULL AND scheduled_end_at IS NOT NULL);

-- A schedule-less trip (route-slot claims from the Driver app never set one;
-- so can a plain POST /trips) has no window for the overlap constraints above
-- to compare, so it falls through them entirely. Without a separate guard,
-- two schedule-less trips for the same route/bus/driver/date/type could both
-- be created (this regressed exactly that: TestDriverSelfAssignment and
-- TestConcurrentRouteClaims both caught two claims on the same route
-- succeeding where the old trips_route_slot_key unique index used to make
-- the second one 409). These indexes restore that original one-per-slot
-- guarantee, scoped to only the schedule-less rows, alongside (not instead
-- of) the time-range constraints above for scheduled rows. Same names as the
-- indexes they replace: handlers/route_slots.go's claimConflicts and
-- handlers/trips.go's tripSlotConflicts already key off them.
CREATE UNIQUE INDEX trips_route_slot_key  ON trips (route_id,  trip_date, trip_type)
    WHERE status <> 'cancelled' AND scheduled_start_at IS NULL;
CREATE UNIQUE INDEX trips_bus_slot_key    ON trips (bus_id,    trip_date, trip_type)
    WHERE status <> 'cancelled' AND scheduled_start_at IS NULL;
CREATE UNIQUE INDEX trips_driver_slot_key ON trips (driver_id, trip_date, trip_type)
    WHERE status <> 'cancelled' AND scheduled_start_at IS NULL;

-- trips_driver_started_key (one trip 'started' at a time per driver) is
-- unchanged and still applies.

-- +goose Down
DROP INDEX trips_driver_slot_key;
DROP INDEX trips_bus_slot_key;
DROP INDEX trips_route_slot_key;
ALTER TABLE trips DROP CONSTRAINT trips_driver_no_overlap;
ALTER TABLE trips DROP CONSTRAINT trips_bus_no_overlap;
ALTER TABLE trips DROP CONSTRAINT trips_route_no_overlap;
CREATE UNIQUE INDEX trips_route_slot_key  ON trips (route_id,  trip_date, trip_type) WHERE status <> 'cancelled';
CREATE UNIQUE INDEX trips_bus_slot_key    ON trips (bus_id,    trip_date, trip_type) WHERE status <> 'cancelled';
CREATE UNIQUE INDEX trips_driver_slot_key ON trips (driver_id, trip_date, trip_type) WHERE status <> 'cancelled';
ALTER TABLE trips DROP CONSTRAINT trips_scheduled_window_order;
ALTER TABLE trips DROP COLUMN scheduled_end_at;
ALTER TABLE trips DROP COLUMN scheduled_start_at;
