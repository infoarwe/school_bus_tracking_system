-- +goose Up
-- FCM topic migration (S?-??): push delivery moves from per-device tokens to
-- per-topic sends; device tokens remain, but only for subscription management.

-- Ledger of which topics a device is currently subscribed to, so reconciliation
-- can diff "desired" vs "actual" without re-querying Firebase. Cascades with the
-- token: if a device_tokens row is ever deleted through another path, its
-- tracked subscriptions are cleaned up too (the handler still unsubscribes with
-- Firebase first — this cascade is a safety net, not the primary mechanism).
CREATE TABLE device_topic_subscriptions (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token      text        NOT NULL REFERENCES device_tokens (token) ON DELETE CASCADE,
    topic      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (token, topic)
);
CREATE INDEX device_topic_subscriptions_topic_idx ON device_topic_subscriptions (topic);

-- Push queue and delivery log: one row per (notification, topic) — a topic send
-- reaches every subscriber in one Firebase call, so this is no longer one row
-- per device. 'config_missing': the school has no real Firebase project
-- configured (LogSender fallback) — distinct from 'failed' so it is never
-- mistaken for a delivered push, and is not endlessly retried like a transient
-- error.
CREATE TABLE topic_deliveries (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id uuid        NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    topic           text        NOT NULL,
    status          text        NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'sent', 'failed', 'config_missing')),
    attempts        int         NOT NULL DEFAULT 0,
    last_error      text        NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    sent_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX topic_deliveries_queue_idx ON topic_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX topic_deliveries_notification_idx ON topic_deliveries (notification_id);

-- +goose Down
DROP TABLE topic_deliveries;
DROP TABLE device_topic_subscriptions;
