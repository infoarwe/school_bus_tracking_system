-- +goose Up
-- Allow Google ETA corrections down to every 5 s (the Driver app uploads every 5–10 s,
-- so faster would not help). Was 10–3600 in 00016.
ALTER TABLE schools DROP CONSTRAINT schools_google_eta_refresh_seconds_check;
ALTER TABLE schools ADD CONSTRAINT schools_google_eta_refresh_seconds_check
    CHECK (google_eta_refresh_seconds = 0 OR google_eta_refresh_seconds BETWEEN 5 AND 3600);

-- +goose Down
UPDATE schools SET google_eta_refresh_seconds = 10 WHERE google_eta_refresh_seconds BETWEEN 1 AND 9;
ALTER TABLE schools DROP CONSTRAINT schools_google_eta_refresh_seconds_check;
ALTER TABLE schools ADD CONSTRAINT schools_google_eta_refresh_seconds_check
    CHECK (google_eta_refresh_seconds = 0 OR google_eta_refresh_seconds BETWEEN 10 AND 3600);
