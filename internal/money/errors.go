package money

import "errors"

var (
	ErrInvalidMoney        = errors.New("invalid money")
	ErrInvalidCurrency     = errors.New("invalid currency")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrNegativeExternal    = errors.New("negative external amount")
	ErrOverflow            = errors.New("money overflow")
	ErrScaleExceeded       = errors.New("scale exceeds two decimal places")
	ErrScientificNotation  = errors.New("scientific notation is not allowed")
	ErrUnsupportedCurrency = errors.New("unsupported currency")
)
