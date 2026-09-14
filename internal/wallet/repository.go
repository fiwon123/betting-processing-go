package wallet

import (
	"context"

	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/fiwon123/betting-processing-go/internal/money"
)

type Repository interface {
	Create(ctx context.Context, w *Wallet) error

	FindByID(ctx context.Context, id string) (*Wallet, error)

	FindByIDForUpdate(ctx context.Context, tx domain.DBTx, id string) (*Wallet, error)

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

	UpdateBalanceTx(
		ctx context.Context,
		tx domain.DBTx,
		id string,
		newBalance money.Money,
		expectedVersion int64,
	) (newVersion int64, err error)

	CreateWalletTx(
		ctx context.Context,
		tx domain.DBTx,
		playerID string,
		providerID string,
		currency money.Currency,
		balance money.Money,
	) (walletID string, err error)

	CreateOpeningTx(
		ctx context.Context,
		tx domain.DBTx,
		walletID string,
		playerID string,
		amount money.Money,
		currency money.Currency,
	) (txID string, err error)

	CreateLedgerEntryTx(
		ctx context.Context,
		tx domain.DBTx,
		entry *LedgerEntry,
	) (entryID string, err error)

	CreateOutboxEventTx(
		ctx context.Context,
		tx domain.DBTx,
		aggregateType string,
		aggregateID string,
		eventType string,
		payload []byte,
	) error
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
