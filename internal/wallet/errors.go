package wallet

import "errors"

var (
	ErrInvalidWalletID        = errors.New("invalid_wallet_id")
	ErrInvalidPlayerID        = errors.New("invalid_player_id")
	ErrInvalidProviderID      = errors.New("invalid_provider_id")
	ErrInvalidCurrency        = errors.New("invalid_currency")
	ErrCurrencyMismatch       = errors.New("currency_mismatch")
	ErrNegativeBalance        = errors.New("negative_balance")
	ErrInsufficientBalance    = errors.New("insufficient_balance")
	ErrPositiveAmountRequired = errors.New("positive_amount_required")
	ErrInvalidWalletVersion   = errors.New("invalid_wallet_version")
	ErrInvalidWalletTimestamp = errors.New("invalid_wallet_timestamp")
	ErrInvalidLedgerEntry     = errors.New("invalid_ledger_entry")
	ErrLedgerMathInvalid      = errors.New("ledger_math_invalid")
	ErrWalletNotFound         = errors.New("wallet_not_found")
	ErrDuplicateWallet        = errors.New("duplicate_wallet")
	ErrConcurrentUpdate       = errors.New("concurrent_update")
)
