-- +goose Up
-- S8-09: per-school branding for the school's white-label Parent and Driver apps.
-- Empty name/colours = the app's built-in defaults. The logo file lives in file
-- storage (internal/storage) under brand_logo_key; only the key is stored here.
ALTER TABLE schools
    ADD COLUMN brand_app_name        text NOT NULL DEFAULT '' CHECK (length(brand_app_name) <= 60),
    ADD COLUMN brand_primary_color   text NOT NULL DEFAULT '' CHECK (brand_primary_color ~ '^(#[0-9A-F]{6})?$'),
    ADD COLUMN brand_secondary_color text NOT NULL DEFAULT '' CHECK (brand_secondary_color ~ '^(#[0-9A-F]{6})?$'),
    ADD COLUMN brand_logo_key        text,
    ADD COLUMN brand_updated_at      timestamptz;

CREATE UNIQUE INDEX schools_brand_logo_key ON schools (brand_logo_key) WHERE brand_logo_key IS NOT NULL;

-- +goose Down
DROP INDEX schools_brand_logo_key;
ALTER TABLE schools
    DROP COLUMN brand_updated_at,
    DROP COLUMN brand_logo_key,
    DROP COLUMN brand_secondary_color,
    DROP COLUMN brand_primary_color,
    DROP COLUMN brand_app_name;
