-- +goose Up
-- Sprint 1: schools (tenants), users with roles, sessions, OTP codes, audit log.
-- Roles are a fixed set enforced by a CHECK constraint rather than a roles table.

CREATE TABLE schools (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text        NOT NULL,
    code             citext      NOT NULL UNIQUE,
    address          text        NOT NULL DEFAULT '',
    city             text        NOT NULL DEFAULT '',
    state            text        NOT NULL DEFAULT '',
    pincode          text        NOT NULL DEFAULT '',
    contact_name     text        NOT NULL DEFAULT '',
    contact_phone    text        NOT NULL DEFAULT '',
    contact_email    text        NOT NULL DEFAULT '',
    working_days     text[]      NOT NULL DEFAULT '{mon,tue,wed,thu,fri}',
    timezone         text        NOT NULL DEFAULT 'Asia/Kolkata',
    transport_config jsonb       NOT NULL DEFAULT '{}',
    status           text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER schools_updated_at BEFORE UPDATE ON schools
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid REFERENCES schools (id),
    role          text        NOT NULL CHECK (role IN ('super_admin', 'school_admin', 'transport_manager', 'driver', 'parent')),
    name          text        NOT NULL,
    email         citext,
    mobile        text,
    password_hash text,
    status        text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    totp_secret   text,
    totp_enabled  boolean     NOT NULL DEFAULT false,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- Every operational user belongs to exactly one school; only Super Admin is global.
    CONSTRAINT users_school_scope CHECK ((role = 'super_admin') = (school_id IS NULL))
);
CREATE UNIQUE INDEX users_email_key ON users (email) WHERE email IS NOT NULL;
-- One login identity per mobile per role (a person may be both a driver and a parent).
CREATE UNIQUE INDEX users_mobile_role_key ON users (mobile, role) WHERE mobile IS NOT NULL;
CREATE INDEX users_school_role_idx ON users (school_id, role);
CREATE TRIGGER users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE user_sessions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token_hash text        NOT NULL UNIQUE,
    device_name        text        NOT NULL DEFAULT '',
    user_agent         text        NOT NULL DEFAULT '',
    ip                 text        NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_used_at       timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz
);
CREATE INDEX user_sessions_user_idx ON user_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE otp_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mobile      text        NOT NULL,
    role        text        NOT NULL,
    code_hash   text        NOT NULL,
    attempts    int         NOT NULL DEFAULT 0,
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX otp_codes_lookup_idx ON otp_codes (mobile, role, created_at DESC);

CREATE TABLE audit_logs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id     uuid REFERENCES schools (id),
    actor_user_id uuid REFERENCES users (id),
    actor_role    text        NOT NULL DEFAULT '',
    action        text        NOT NULL,
    entity_type   text        NOT NULL,
    entity_id     uuid,
    before        jsonb,
    after         jsonb,
    ip            text        NOT NULL DEFAULT '',
    request_id    text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_school_created_idx ON audit_logs (school_id, created_at DESC);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity_type, entity_id);

-- +goose Down
DROP TABLE audit_logs;
DROP TABLE otp_codes;
DROP TABLE user_sessions;
DROP TABLE users;
DROP TABLE schools;
