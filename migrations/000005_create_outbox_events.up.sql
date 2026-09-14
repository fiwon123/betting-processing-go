CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,

    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,

    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,

    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,

    CONSTRAINT outbox_attempts_chk
        CHECK (attempts >= 0)
);

CREATE INDEX outbox_pending_idx
    ON outbox_events (next_attempt_at)
    WHERE published_at IS NULL;