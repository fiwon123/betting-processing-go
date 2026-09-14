ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_player_id_currency_provider_id_key;

ALTER TABLE wallets DROP COLUMN IF EXISTS provider_id;

ALTER TABLE wallets ADD CONSTRAINT wallets_player_id_currency_key
    UNIQUE (player_id, currency);
