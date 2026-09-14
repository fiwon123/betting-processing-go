package wallet

import (
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

type Direction string

const (
	DEBIT  Direction = "DEBIT"
	CREDIT Direction = "CREDIT"
)

type LedgerEntry struct {
	id            string
	walletID      string
	transactionID string
	direction     Direction
	amount        money.Money
	currency      money.Currency
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

func NewLedgerEntry(
	id string,
	walletID string,
	transactionID string,
	direction Direction,
	amount money.Money,
	balanceBefore money.Money,
	balanceAfter money.Money,
) (*LedgerEntry, error) {
	if walletID == "" || transactionID == "" {
		return nil, ErrInvalidLedgerEntry
	}

	if direction != DEBIT && direction != CREDIT {
		return nil, ErrInvalidLedgerEntry
	}

	if amount.IsNegative() || amount.IsZero() {
		return nil, ErrPositiveAmountRequired
	}

	if balanceBefore.IsNegative() || balanceAfter.IsNegative() {
		return nil, ErrNegativeBalance
	}

	if amount.Currency() != balanceBefore.Currency() ||
		amount.Currency() != balanceAfter.Currency() {
		return nil, ErrCurrencyMismatch
	}

	var expectedAfter money.Money
	var err error

	switch direction {
	case CREDIT:
		expectedAfter, err = balanceBefore.Add(amount)
	case DEBIT:
		expectedAfter, err = balanceBefore.Subtract(amount)
	}

	if err != nil {
		return nil, err
	}

	cmp, err := expectedAfter.Compare(balanceAfter)
	if err != nil {
		return nil, err
	}

	if cmp != 0 {
		return nil, ErrLedgerMathInvalid
	}

	return &LedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		currency:      amount.Currency(),
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     time.Now().UTC(),
	}, nil
}

func RehydrateLedgerEntry(
	id string,
	walletID string,
	transactionID string,
	direction Direction,
	amount money.Money,
	balanceBefore money.Money,
	balanceAfter money.Money,
	createdAt time.Time,
) (*LedgerEntry, error) {
	if id == "" || walletID == "" || transactionID == "" {
		return nil, ErrInvalidLedgerEntry
	}

	if direction != DEBIT && direction != CREDIT {
		return nil, ErrInvalidLedgerEntry
	}

	if amount.IsNegative() || amount.IsZero() {
		return nil, ErrPositiveAmountRequired
	}

	if balanceBefore.IsNegative() || balanceAfter.IsNegative() {
		return nil, ErrNegativeBalance
	}

	if amount.Currency() != balanceBefore.Currency() ||
		amount.Currency() != balanceAfter.Currency() {
		return nil, ErrCurrencyMismatch
	}

	if createdAt.IsZero() {
		return nil, ErrInvalidWalletTimestamp
	}

	var expectedAfter money.Money
	var err error

	switch direction {
	case CREDIT:
		expectedAfter, err = balanceBefore.Add(amount)
	case DEBIT:
		expectedAfter, err = balanceBefore.Subtract(amount)
	}

	if err != nil {
		return nil, err
	}

	cmp, err := expectedAfter.Compare(balanceAfter)
	if err != nil {
		return nil, err
	}

	if cmp != 0 {
		return nil, ErrLedgerMathInvalid
	}

	return &LedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		currency:      amount.Currency(),
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt.UTC(),
	}, nil
}

func (e *LedgerEntry) ID() string {
	return e.id
}

func (e *LedgerEntry) WalletID() string {
	return e.walletID
}

func (e *LedgerEntry) TransactionID() string {
	return e.transactionID
}

func (e *LedgerEntry) Direction() Direction {
	return e.direction
}

func (e *LedgerEntry) Amount() money.Money {
	return e.amount
}

func (e *LedgerEntry) Currency() money.Currency {
	return e.currency
}

func (e *LedgerEntry) BalanceBefore() money.Money {
	return e.balanceBefore
}

func (e *LedgerEntry) BalanceAfter() money.Money {
	return e.balanceAfter
}

func (e *LedgerEntry) CreatedAt() time.Time {
	return e.createdAt
}
