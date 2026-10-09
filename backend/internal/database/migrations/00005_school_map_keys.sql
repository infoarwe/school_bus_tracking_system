-- +goose Up
-- Google Maps keys differ per school.
-- Browser key: used by the admin web to show maps (visible to the school's staff; restrict it by HTTP referrer in Google Cloud).
-- Server key: used by the backend for ETA/routing (Sprint 6). Encrypted at rest and never returned by the API.
ALTER TABLE schools
    ADD COLUMN maps_browser_key    text  NOT NULL DEFAULT '',
    ADD COLUMN maps_server_key_enc bytea;

-- +goose Down
ALTER TABLE schools
    DROP COLUMN maps_server_key_enc,
    DROP COLUMN maps_browser_key;
