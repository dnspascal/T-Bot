CREATE TABLE subscribers (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT,
    status     TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
