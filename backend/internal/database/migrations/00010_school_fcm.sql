-- +goose Up
-- Each school uses its own Firebase project for push (its own branded apps).
-- The service-account JSON is stored encrypted (DATA_ENCRYPTION_KEY) and never
-- returned; project ID and client email are kept in clear to show which key is set.
ALTER TABLE schools
    ADD COLUMN fcm_credentials_enc bytea,
    ADD COLUMN fcm_project_id      text NOT NULL DEFAULT '',
    ADD COLUMN fcm_client_email    text NOT NULL DEFAULT '',
    ADD COLUMN fcm_updated_at      timestamptz;

-- +goose Down
ALTER TABLE schools
    DROP COLUMN fcm_updated_at,
    DROP COLUMN fcm_client_email,
    DROP COLUMN fcm_project_id,
    DROP COLUMN fcm_credentials_enc;
