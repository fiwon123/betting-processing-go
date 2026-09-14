CREATE TABLE wallet_ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    wallet_id UUID NOT NULL
        REFERENCES wallets(id),

    transaction_id UUID NOT NULL
        REFERENCES wager_transactions(id),

    direction VARCHAR(10) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,

    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallet_ledger_direction_chk
        CHECK (direction IN ('DEBIT', 'CREDIT')),

    CONSTRAINT wallet_ledger_amount_chk
        CHECK (amount > 0),

    CONSTRAINT wallet_ledger_currency_chk
        CHECK (
            currency = upper(currency)
            AND length(currency) = 3
        ),

    CONSTRAINT wallet_ledger_balance_before_chk
        CHECK (balance_before >= 0),

    CONSTRAINT wallet_ledger_balance_after_chk
        CHECK (balance_after >= 0),

    CONSTRAINT wallet_ledger_math_chk
        CHECK (
            (
                direction = 'CREDIT'
                AND balance_after =
                    balance_before + amount
            )
            OR
            (
                direction = 'DEBIT'
                AND balance_after =
                    balance_before - amount
            )
        ),

    CONSTRAINT wallet_ledger_wallet_transaction_uq
        UNIQUE (wallet_id, transaction_id)
);

CREATE INDEX wallet_ledger_wallet_created_idx
    ON wallet_ledger_entries (wallet_id, created_at);

CREATE OR REPLACE FUNCTION prevent_wallet_ledger_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'wallet ledger entries are immutable';
END;
$$;

CREATE TRIGGER wallet_ledger_no_update
BEFORE UPDATE ON wallet_ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_wallet_ledger_mutation();

CREATE TRIGGER wallet_ledger_no_delete
BEFORE DELETE ON wallet_ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_wallet_ledger_mutation();