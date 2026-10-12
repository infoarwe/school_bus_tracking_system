-- +goose Up
-- Sprint 10: flexible route/trip types. A route can now also support Special
-- Trip/Outing; Evening Drop gets its own independent stop order; trips can be
-- routeless ad-hoc outings with their own per-trip destination list.

ALTER TABLE routes ADD COLUMN supports_outing boolean NOT NULL DEFAULT false; -- Special Trip/Outing
ALTER TABLE routes DROP CONSTRAINT routes_trip_type;
ALTER TABLE routes ADD CONSTRAINT routes_trip_type CHECK (supports_pickup OR supports_drop OR supports_outing);

-- Independent Evening Drop order. NULL = this stop is not part of the evening leg.
-- A plain UNIQUE constraint already lets any number of rows share NULL (standard
-- NULL-distinctness), so no partial/WHERE predicate is needed here — unlike a
-- partial index, a full UNIQUE constraint can be DEFERRABLE, exactly like
-- stops_route_sequence_key, so a reorder can renumber everything inside one
-- transaction without a transient clash.
ALTER TABLE stops ADD COLUMN drop_sequence int CHECK (drop_sequence > 0);
ALTER TABLE stops ADD CONSTRAINT stops_route_drop_sequence_key
    UNIQUE (route_id, drop_sequence) DEFERRABLE INITIALLY DEFERRED;

-- A trip can now be a routeless Special Trip/Outing: route_id becomes optional,
-- and it gets its own title (shown instead of a route name) and an explicit
-- parent-notification opt-in (default on, to match today's behavior for the
-- two existing trip types, which have no such toggle and always notify).
ALTER TABLE trips ALTER COLUMN route_id DROP NOT NULL;
ALTER TABLE trips ADD COLUMN title text NOT NULL DEFAULT '';
ALTER TABLE trips ADD COLUMN notify_parents boolean NOT NULL DEFAULT true;
ALTER TABLE trips DROP CONSTRAINT trips_trip_type_check;
ALTER TABLE trips ADD CONSTRAINT trips_trip_type_check
    CHECK (trip_type IN ('morning_pickup', 'evening_drop', 'special_trip'));
-- DB-safety net matching the handler's validation, so no insert path (including
-- CopyTrips' raw INSERT) can create an inconsistent row:
ALTER TABLE trips ADD CONSTRAINT trips_route_required_unless_special CHECK (
    trip_type = 'special_trip' OR route_id IS NOT NULL
);
ALTER TABLE trips ADD CONSTRAINT trips_title_required_if_routeless_special CHECK (
    trip_type <> 'special_trip' OR route_id IS NOT NULL OR title <> ''
);

-- A Special Trip/Outing's ad-hoc destinations, per trip date — never touches the
-- permanent `stops` table, so a route is never duplicated for an outing.
CREATE TABLE trip_waypoints (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id           uuid NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    school_id         uuid NOT NULL REFERENCES schools (id),
    sequence          int  NOT NULL CHECK (sequence > 0),
    name              text NOT NULL,
    landmark          text NOT NULL DEFAULT '',
    latitude          double precision NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude         double precision NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    geofence_radius_m int  NOT NULL DEFAULT 100 CHECK (geofence_radius_m BETWEEN 25 AND 1000),
    arrived_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT trip_waypoints_trip_sequence_key UNIQUE (trip_id, sequence)
);
CREATE INDEX trip_waypoints_school_idx ON trip_waypoints (school_id);
CREATE TRIGGER trip_waypoints_updated_at BEFORE UPDATE ON trip_waypoints
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE trip_waypoints;
ALTER TABLE trips DROP CONSTRAINT trips_title_required_if_routeless_special;
ALTER TABLE trips DROP CONSTRAINT trips_route_required_unless_special;
ALTER TABLE trips DROP CONSTRAINT trips_trip_type_check;
ALTER TABLE trips ADD CONSTRAINT trips_trip_type_check CHECK (trip_type IN ('morning_pickup', 'evening_drop'));
ALTER TABLE trips DROP COLUMN notify_parents;
ALTER TABLE trips DROP COLUMN title;
ALTER TABLE trips ALTER COLUMN route_id SET NOT NULL;
ALTER TABLE stops DROP CONSTRAINT stops_route_drop_sequence_key;
ALTER TABLE stops DROP COLUMN drop_sequence;
ALTER TABLE routes DROP CONSTRAINT routes_trip_type;
ALTER TABLE routes ADD CONSTRAINT routes_trip_type CHECK (supports_pickup OR supports_drop);
ALTER TABLE routes DROP COLUMN supports_outing;
