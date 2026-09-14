package wagertransaction

import (
	"errors"
)

var (
	ErrInvalidTransactionID   = errors.New("invalid_transaction_id")
	ErrInvalidProvider        = errors.New("invalid_provider")
	ErrInvalidExternalID      = errors.New("invalid_external_id")
	ErrInvalidIdempotencyKey  = errors.New("invalid_idempotency_key")
	ErrInvalidWalletID        = errors.New("invalid_wallet_id")
	ErrInvalidPlayerID        = errors.New("invalid_player_id")
	ErrInvalidTransactionType = errors.New("invalid_transaction_type")
	ErrInvalidMoney           = errors.New("invalid_money")
	ErrOPENINGRejected        = errors.New("opening_rejected_external")
	ErrPositiveAmountRequired = errors.New("positive_amount_required")
	ErrZeroAmountRequired     = errors.New("zero_amount_required")
	ErrInvalidStateTransition = errors.New("invalid_state_transition")
	ErrTransactionNotFound    = errors.New("transaction_not_found")
	ErrPayloadConflict        = errors.New("payload_conflict")
	ErrInsufficientBalance    = errors.New("insufficient_balance")
	ErrReferenceNotFound      = errors.New("reference_not_found")
	ErrReferenceNotSuccessful = errors.New("reference_not_successful")
	ErrDoubleReversal         = errors.New("double_reversal")
	ErrProviderMismatch       = errors.New("provider_mismatch")
	ErrPlayerMismatch         = errors.New("player_mismatch")
	ErrWalletMismatch         = errors.New("wallet_mismatch")
	ErrCurrencyMismatch       = errors.New("currency_mismatch")
	ErrRoundMismatch          = errors.New("round_mismatch")
	ErrReversalValueMismatch  = errors.New("reversal_value_mismatch")
	ErrIdempotentDuplicate    = errors.New("idempotent_duplicate")
	ErrExternalIDReuse        = errors.New("external_id_reuse")
)

// IsTerminalBusinessError returns true for errors that represent definitive
// business rule violations. These should cause the SQS message to be deleted
// since retrying will never succeed.
func IsTerminalBusinessError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrInvalidTransactionType) ||
		errors.Is(err, ErrInvalidMoney) ||
		errors.Is(err, ErrOPENINGRejected) ||
		errors.Is(err, ErrPositiveAmountRequired) ||
		errors.Is(err, ErrZeroAmountRequired) ||
		errors.Is(err, ErrPayloadConflict) ||
		errors.Is(err, ErrInsufficientBalance) ||
		errors.Is(err, ErrReferenceNotFound) ||
		errors.Is(err, ErrReferenceNotSuccessful) ||
		errors.Is(err, ErrDoubleReversal) ||
		errors.Is(err, ErrProviderMismatch) ||
		errors.Is(err, ErrPlayerMismatch) ||
		errors.Is(err, ErrWalletMismatch) ||
		errors.Is(err, ErrCurrencyMismatch) ||
		errors.Is(err, ErrRoundMismatch) ||
		errors.Is(err, ErrReversalValueMismatch) ||
		errors.Is(err, ErrInvalidStateTransition) ||
		errors.Is(err, ErrIdempotentDuplicate) ||
		errors.Is(err, ErrExternalIDReuse)
}
