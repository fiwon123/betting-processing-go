ALTER TABLE wallets ADD CONSTRAINT wallets_player_currency_uq UNIQUE (player_id, currency);
