-- +goose Up
-- Sprint 2: transport master data. All rows carry school_id for tenant scoping.
-- Drivers, buses and routes are deactivated, never deleted, so trip history stays intact.

-- A driver's name and mobile live on their users row (the OTP login identity).
CREATE TABLE drivers (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id               uuid        NOT NULL REFERENCES schools (id),
    user_id                 uuid        NOT NULL UNIQUE REFERENCES users (id),
    license_number          text        NOT NULL DEFAULT '',
    license_expiry          date,
    address                 text        NOT NULL DEFAULT '',
    emergency_contact_name  text        NOT NULL DEFAULT '',
    emergency_contact_phone text        NOT NULL DEFAULT '',
    id_proof_type           text        NOT NULL DEFAULT '',
    id_proof_number         text        NOT NULL DEFAULT '',
    notes                   text        NOT NULL DEFAULT '',
    status                  text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'suspended')),
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX drivers_school_idx ON drivers (school_id, status);
CREATE TRIGGER drivers_updated_at BEFORE UPDATE ON drivers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE buses (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id      uuid        NOT NULL REFERENCES schools (id),
    vehicle_number text        NOT NULL,
    capacity       int         NOT NULL CHECK (capacity BETWEEN 1 AND 100),
    make_model     text        NOT NULL DEFAULT '',
    gps_device_id  text        NOT NULL DEFAULT '',
    notes          text        NOT NULL DEFAULT '',
    status         text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'maintenance')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
-- "TN-38-AB-1234" and "tn 38 ab 1234" are the same bus.
CREATE UNIQUE INDEX buses_school_vehicle_key
    ON buses (school_id, regexp_replace(upper(vehicle_number), '[^A-Z0-9]', '', 'g'));
CREATE TRIGGER buses_updated_at BEFORE UPDATE ON buses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE routes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    name            text        NOT NULL,
    code            citext      NOT NULL,
    start_point     text        NOT NULL DEFAULT '',
    description     text        NOT NULL DEFAULT '',
    supports_pickup boolean     NOT NULL DEFAULT true,  -- Morning Pickup
    supports_drop   boolean     NOT NULL DEFAULT true,  -- Evening Drop
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT routes_school_code_key UNIQUE (school_id, code),
    CONSTRAINT routes_trip_type CHECK (supports_pickup OR supports_drop)
);
CREATE TRIGGER routes_updated_at BEFORE UPDATE ON routes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE stops (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid             NOT NULL REFERENCES schools (id),
    route_id          uuid             NOT NULL REFERENCES routes (id) ON DELETE CASCADE,
    name              text             NOT NULL,
    landmark          text             NOT NULL DEFAULT '',
    latitude          double precision NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude         double precision NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    sequence          int              NOT NULL CHECK (sequence > 0),
    pickup_time       time,  -- scheduled time at this stop for Morning Pickup
    drop_time         time,  -- scheduled time at this stop for Evening Drop
    geofence_radius_m int              NOT NULL DEFAULT 100 CHECK (geofence_radius_m BETWEEN 25 AND 1000),
    created_at        timestamptz      NOT NULL DEFAULT now(),
    updated_at        timestamptz      NOT NULL DEFAULT now(),
    -- Deferred so a reorder can renumber all stops inside one transaction.
    CONSTRAINT stops_route_sequence_key UNIQUE (route_id, sequence) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX stops_school_idx ON stops (school_id);
CREATE TRIGGER stops_updated_at BEFORE UPDATE ON stops
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE stops;
DROP TABLE routes;
DROP TABLE buses;
DROP TABLE drivers;
