CREATE TABLE inbox_messages (
    consumer_name TEXT NOT NULL,
    message_id TEXT NOT NULL,

    payload_hash TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,

    PRIMARY KEY (consumer_name, message_id)
);

CREATE INDEX inbox_messages_incomplete_idx
    ON inbox_messages (received_at)
    WHERE completed_at IS NULL;