-- +goose Up
-- Sprint 7: push notifications, inbox, announcements, delays and emergencies.

-- App devices that can receive push (FCM). Tied to the login session: a device
-- whose session ended (logout, suspension) receives nothing.
CREATE TABLE device_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    session_id   uuid        REFERENCES user_sessions (id) ON DELETE CASCADE,
    token        text        NOT NULL UNIQUE,
    platform     text        NOT NULL CHECK (platform IN ('android', 'ios')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX device_tokens_user_idx ON device_tokens (user_id);

-- One row per notification (automatic, announcement, delay...).
CREATE TABLE notifications (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    type            text        NOT NULL, -- bus_started, bus_approaching, ..., announcement
    title           text        NOT NULL,
    body            text        NOT NULL,
    data            jsonb       NOT NULL DEFAULT '{}', -- trip_id, route_id, stop_id, announcement_id...
    trip_id         uuid        REFERENCES trips (id) ON DELETE SET NULL,
    route_id        uuid        REFERENCES routes (id),
    -- Automatic notifications fire once (e.g. "trip:<id>:stop:<id>:approaching").
    dedupe_key      text        UNIQUE,
    recipient_count int         NOT NULL DEFAULT 0,
    created_by      uuid        REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_school_idx ON notifications (school_id, created_at DESC);

-- The inbox: one row per recipient user and child the notification is about
-- (student_id NULL = not about one child, e.g. a school-wide announcement).
CREATE TABLE notification_recipients (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id uuid        NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    student_id      uuid        REFERENCES students (id) ON DELETE CASCADE,
    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_recipients_once UNIQUE NULLS NOT DISTINCT (notification_id, user_id, student_id)
);
CREATE INDEX notification_recipients_user_idx ON notification_recipients (user_id, created_at DESC);

-- Push queue and delivery log: one row per device per notification.
CREATE TABLE push_deliveries (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id uuid        NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token           text        NOT NULL,
    title           text        NOT NULL, -- personalised (child names)
    body            text        NOT NULL,
    data            jsonb       NOT NULL DEFAULT '{}',
    status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'invalid')),
    attempts        int         NOT NULL DEFAULT 0,
    last_error      text        NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX push_deliveries_queue_idx ON push_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX push_deliveries_notification_idx ON push_deliveries (notification_id);

CREATE TABLE announcements (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    category        text        NOT NULL CHECK (category IN ('school_announcement', 'route_announcement', 'bus_breakdown',
                        'traffic_delay', 'pickup_change', 'emergency_message', 'holiday', 'other')),
    target          text        NOT NULL CHECK (target IN ('school', 'route')),
    route_id        uuid        REFERENCES routes (id),
    title           text        NOT NULL,
    message         text        NOT NULL,
    attachment_url  text        NOT NULL DEFAULT '',
    scheduled_at    timestamptz, -- null = send now
    status          text        NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'sending', 'sent', 'cancelled')),
    sent_at         timestamptz,
    notification_id uuid        REFERENCES notifications (id),
    created_by      uuid        REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT announcements_route_target CHECK ((target = 'route') = (route_id IS NOT NULL))
);
CREATE INDEX announcements_school_idx ON announcements (school_id, created_at DESC);
CREATE INDEX announcements_due_idx ON announcements (scheduled_at) WHERE status = 'scheduled';
CREATE TRIGGER announcements_updated_at BEFORE UPDATE ON announcements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE delay_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    trip_id         uuid        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    minutes         int         NOT NULL CHECK (minutes BETWEEN 1 AND 240),
    reason          text        NOT NULL CHECK (reason IN ('traffic', 'bus_breakdown', 'road_block', 'weather', 'driver_issue', 'other')),
    note            text        NOT NULL DEFAULT '',
    reported_by     uuid        REFERENCES users (id),
    reporter_role   text        NOT NULL,
    notification_id uuid        REFERENCES notifications (id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX delay_events_school_idx ON delay_events (school_id, created_at DESC);

CREATE TABLE emergencies (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid        NOT NULL REFERENCES schools (id),
    trip_id         uuid        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    message         text        NOT NULL,
    latitude        double precision,
    longitude       double precision,
    status          text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved')),
    reported_by     uuid        REFERENCES users (id),
    acknowledged_by uuid        REFERENCES users (id),
    acknowledged_at timestamptz,
    resolved_by     uuid        REFERENCES users (id),
    resolved_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX emergencies_open_idx ON emergencies (school_id, created_at DESC) WHERE status <> 'resolved';
CREATE TRIGGER emergencies_updated_at BEFORE UPDATE ON emergencies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE emergencies;
DROP TABLE delay_events;
DROP TABLE announcements;
DROP TABLE push_deliveries;
DROP TABLE notification_recipients;
DROP TABLE notifications;
DROP TABLE device_tokens;
