-- +goose Up
-- How often live ETAs are corrected with Google Routes (billed per call), per school:
-- 0 = never (own estimate only), else 10–3600 seconds per trip. Settings → Live tracking.
ALTER TABLE schools
    ADD COLUMN google_eta_refresh_seconds int NOT NULL DEFAULT 300
        CHECK (google_eta_refresh_seconds = 0 OR google_eta_refresh_seconds BETWEEN 10 AND 3600);

-- +goose Down
ALTER TABLE schools DROP COLUMN google_eta_refresh_seconds;
