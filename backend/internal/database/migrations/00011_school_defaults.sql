-- +goose Up
-- Sprint 8: school-level default arrival radius for new stops (Settings → Live tracking).
ALTER TABLE schools
    ADD COLUMN default_geofence_m int NOT NULL DEFAULT 100 CHECK (default_geofence_m BETWEEN 25 AND 1000);

-- +goose Down
ALTER TABLE schools DROP COLUMN default_geofence_m;
