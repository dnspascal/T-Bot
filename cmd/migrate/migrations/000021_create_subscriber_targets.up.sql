CREATE TABLE subscriber_targets (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id UUID        NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    channel       TEXT        NOT NULL,
    recipient_id  TEXT        NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(subscriber_id, channel)
);

CREATE INDEX ON subscriber_targets(channel, status);
