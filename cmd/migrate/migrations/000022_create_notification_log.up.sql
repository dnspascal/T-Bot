CREATE TABLE notification_log (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id UUID        REFERENCES subscribers(id) ON DELETE SET NULL,
    channel       TEXT        NOT NULL,
    type          TEXT        NOT NULL,
    payload       JSONB,
    status        TEXT        NOT NULL,
    error         TEXT,
    sent_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ON notification_log(sent_at DESC);
CREATE INDEX ON notification_log(subscriber_id, sent_at DESC);
