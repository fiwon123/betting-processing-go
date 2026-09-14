ALTER TABLE wager_transactions
    ADD COLUMN ref_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE wager_transactions
    ADD COLUMN ref_next_attempt_at TIMESTAMPTZ;
