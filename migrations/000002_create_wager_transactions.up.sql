CREATE TABLE wager_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    origin VARCHAR(20) NOT NULL DEFAULT 'EXTERNAL',

    external_id TEXT,
    provider TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,

    wallet_id UUID NOT NULL
        REFERENCES wallets(id),

    player_id UUID NOT NULL,

    round_id TEXT,
    game_id TEXT,

    transaction_type VARCHAR(20) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,

    external_reference TEXT,
    internal_reference TEXT,

    status VARCHAR(30) NOT NULL DEFAULT 'PENDING',
    failure_code TEXT,
    result_json JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,

    CONSTRAINT wager_transactions_origin_chk
        CHECK (origin IN ('INTERNAL', 'EXTERNAL')),

    CONSTRAINT wager_transactions_type_chk
        CHECK (
            transaction_type IN (
                'OPENING',
                'BET',
                'WIN',
                'LOSS',
                'REFUND',
                'ROLLBACK'
            )
        ),

    CONSTRAINT wager_transactions_status_chk
        CHECK (
            status IN (
                'PENDING',
                'PENDING_REFERENCE',
                'PROCESSED',
                'REJECTED',
                'FAILED'
            )
        ),

    CONSTRAINT wager_transactions_amount_chk
        CHECK (amount >= 0),

    CONSTRAINT wager_transactions_currency_chk
        CHECK (
            currency = upper(currency)
            AND length(currency) = 3
        ),

    CONSTRAINT wager_transactions_opening_rules_chk
        CHECK (
            (
                transaction_type = 'OPENING'
                AND origin = 'INTERNAL'
                AND external_id IS NULL
                AND provider IS NULL
                AND idempotency_key IS NULL
                AND payload_hash IS NULL
                AND round_id IS NULL
                AND game_id IS NULL
                AND external_reference IS NULL
            )
            OR
            (
                transaction_type <> 'OPENING'
                AND origin = 'EXTERNAL'
            )
        )
);

CREATE UNIQUE INDEX wager_transactions_idempotency_uq
    ON wager_transactions (provider, idempotency_key)
    WHERE provider IS NOT NULL
      AND idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX wager_transactions_one_opening_per_wallet_uq
    ON wager_transactions (wallet_id)
    WHERE transaction_type = 'OPENING';

CREATE INDEX wager_transactions_wallet_id_idx
    ON wager_transactions (wallet_id);

CREATE INDEX wager_transactions_status_idx
    ON wager_transactions (status);

CREATE INDEX wager_transactions_round_id_idx
    ON wager_transactions (round_id)
    WHERE round_id IS NOT NULL;