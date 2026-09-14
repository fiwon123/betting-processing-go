ALTER TABLE wallets ADD COLUMN provider_id VARCHAR(255) NOT NULL DEFAULT '';

CREATE INDEX wallets_provider_id_idx ON wallets (provider_id);

ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_player_id_currency_key;

ALTER TABLE wallets ADD CONSTRAINT wallets_player_id_currency_provider_id_key
    UNIQUE (player_id, currency, provider_id);
