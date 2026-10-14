-- +goose Up
-- Travelled-route snapshot made when a trip is completed: a map image (Google Static Maps,
-- stored through storage.Store at school_{school_id}/trips/{trip_id}.png) plus the simplified
-- path and totals, so the route stays viewable after the GPS history (trip_locations)
-- passes the school's retention period. One row per trip; regenerating replaces it.

CREATE TABLE trip_route_snapshots (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id      uuid        NOT NULL UNIQUE REFERENCES trips (id) ON DELETE CASCADE,
    school_id    uuid        NOT NULL REFERENCES schools (id),
    -- ready: image stored; no_gps: no stored GPS points (or history is off); failed: see error
    status       text        NOT NULL CHECK (status IN ('ready', 'no_gps', 'failed')),
    image_key    text,
    -- Google encoded polyline of the travelled path (simplified), for redrawing without GPS history
    polyline     text        NOT NULL DEFAULT '',
    distance_m   int         NOT NULL DEFAULT 0,
    point_count  int         NOT NULL DEFAULT 0,
    started_at   timestamptz,
    ended_at     timestamptz,
    error        text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trip_route_snapshots_school_idx ON trip_route_snapshots (school_id);
CREATE TRIGGER trip_route_snapshots_updated_at BEFORE UPDATE ON trip_route_snapshots
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE trip_route_snapshots;
