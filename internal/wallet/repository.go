package wallet

import (
	"context"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

type Repository interface {
	Create(ctx context.Context, w *Wallet) error

	FindByID(ctx context.Context, id string) (*Wallet, error)

	FindByPlayerAndCurrencyAndProvider(
		ctx context.Context,
		playerID string,
		currency money.Currency,
		providerID string,
	) (*Wallet, error)

	UpdateBalance(
		ctx context.Context,
		id string,
		newBalance money.Money,
		expectedVersion int64,
	) (newVersion int64, err error)
}

type LedgerRepository interface {
	Create(ctx context.Context, entry *LedgerEntry) error

	FindByWalletID(
		ctx context.Context,
		walletID string,
		cursor string,
		limit int,
	) ([]*LedgerEntry, string, error)

	SumByWalletID(ctx context.Context, walletID string) (int64, error)
}
