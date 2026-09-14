package wallet

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

const initialWalletVersion int64 = 1

type Wallet struct {
	mu sync.RWMutex

	id         string
	playerID   string
	providerID string
	currency   money.Currency
	balance    money.Money
	version    int64
	createdAt  time.Time
	updatedAt  time.Time
}

func NewWallet(
	id string,
	playerID string,
	providerID string,
	initialBalance money.Money,
) (*Wallet, error) {
	id = strings.TrimSpace(id)
	playerID = strings.TrimSpace(playerID)
	providerID = strings.TrimSpace(providerID)

	if id == "" {
		return nil, ErrInvalidWalletID
	}

	if playerID == "" {
		return nil, ErrInvalidPlayerID
	}

	if providerID == "" {
		return nil, ErrInvalidProviderID
	}

	if initialBalance.Currency() != money.BRL {
		return nil, ErrInvalidCurrency
	}

	if initialBalance.IsNegative() {
		return nil, ErrNegativeBalance
	}

	now := time.Now().UTC()

	return &Wallet{
		id:         id,
		playerID:   playerID,
		providerID: providerID,
		currency:   money.BRL,
		balance:    initialBalance,
		version:    initialWalletVersion,
		createdAt:  now,
		updatedAt:  now,
	}, nil
}

func RehydrateWallet(
	id string,
	playerID string,
	providerID string,
	currency money.Currency,
	balance money.Money,
	version int64,
	createdAt time.Time,
	updatedAt time.Time,
) (*Wallet, error) {
	id = strings.TrimSpace(id)
	playerID = strings.TrimSpace(playerID)
	providerID = strings.TrimSpace(providerID)

	if id == "" {
		return nil, ErrInvalidWalletID
	}

	if playerID == "" {
		return nil, ErrInvalidPlayerID
	}

	if providerID == "" {
		return nil, ErrInvalidProviderID
	}

	if currency != money.BRL {
		return nil, ErrInvalidCurrency
	}

	if balance.Currency() != currency {
		return nil, ErrCurrencyMismatch
	}

	if balance.IsNegative() {
		return nil, ErrNegativeBalance
	}

	if version <= 0 {
		return nil, ErrInvalidWalletVersion
	}

	if createdAt.IsZero() || updatedAt.IsZero() {
		return nil, ErrInvalidWalletTimestamp
	}

	createdAt = createdAt.UTC()
	updatedAt = updatedAt.UTC()

	if updatedAt.Before(createdAt) {
		return nil, ErrInvalidWalletTimestamp
	}

	return &Wallet{
		id:         id,
		playerID:   playerID,
		providerID: providerID,
		currency:   currency,
		balance:    balance,
		version:    version,
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}, nil
}

func (w *Wallet) ID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.id
}

func (w *Wallet) PlayerID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.playerID
}

func (w *Wallet) ProviderID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.providerID
}

func (w *Wallet) Currency() money.Currency {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.currency
}

func (w *Wallet) Balance() money.Money {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.balance
}

func (w *Wallet) Version() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.version
}

func (w *Wallet) CreatedAt() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.createdAt
}

func (w *Wallet) UpdatedAt() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.updatedAt
}

func (w *Wallet) CanDebit(amount money.Money) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.canDebitLocked(amount)
}

func (w *Wallet) canDebitLocked(amount money.Money) bool {
	if amount.Currency() != w.currency {
		return false
	}

	if amount.IsNegative() || amount.IsZero() {
		return false
	}

	cmp, err := w.balance.Compare(amount)
	if err != nil {
		return false
	}

	return cmp >= 0
}

type BalanceChange struct {
	BalanceBefore money.Money
	BalanceAfter  money.Money
	NewVersion    int64
}

func (w *Wallet) Debit(amount money.Money) (*BalanceChange, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if amount.Currency() != w.currency {
		return nil, ErrCurrencyMismatch
	}

	if amount.IsNegative() || amount.IsZero() {
		return nil, ErrPositiveAmountRequired
	}

	if !w.canDebitLocked(amount) {
		return nil, ErrInsufficientBalance
	}

	balanceBefore := w.balance

	newBalance, err := w.balance.Subtract(amount)
	if err != nil {
		return nil, fmt.Errorf("subtract balance: %w", err)
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()

	return &BalanceChange{
		BalanceBefore: balanceBefore,
		BalanceAfter:  newBalance,
		NewVersion:    w.version,
	}, nil
}

func (w *Wallet) Credit(amount money.Money) (*BalanceChange, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if amount.Currency() != w.currency {
		return nil, ErrCurrencyMismatch
	}

	if amount.IsNegative() || amount.IsZero() {
		return nil, ErrPositiveAmountRequired
	}

	balanceBefore := w.balance

	newBalance, err := w.balance.Add(amount)
	if err != nil {
		return nil, fmt.Errorf("add balance: %w", err)
	}

	w.balance = newBalance
	w.version++
	w.updatedAt = time.Now().UTC()

	return &BalanceChange{
		BalanceBefore: balanceBefore,
		BalanceAfter:  newBalance,
		NewVersion:    w.version,
	}, nil
}
