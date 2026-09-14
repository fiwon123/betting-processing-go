CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id UUID NOT NULL,
    currency VARCHAR(3) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallets_currency_chk
        CHECK (
            currency = upper(currency)
            AND length(currency) = 3
        ),

    CONSTRAINT wallets_balance_nonnegative_chk
        CHECK (balance >= 0),

    CONSTRAINT wallets_version_chk
        CHECK (version >= 1),

    CONSTRAINT wallets_player_currency_uq
        UNIQUE (player_id, currency)
);

CREATE INDEX wallets_player_id_idx
    ON wallets (player_id);