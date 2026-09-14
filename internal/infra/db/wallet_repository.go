package db

import (
	"context"
	"fmt"

	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) Create(ctx context.Context, w *wallet.Wallet) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO wallets (id, player_id, provider_id, currency, balance, version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (player_id, currency, provider_id) DO NOTHING`,
		w.ID(), w.PlayerID(), w.ProviderID(), string(w.Currency()), w.Balance().Amount(),
		w.Version(), w.CreatedAt(), w.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) FindByID(ctx context.Context, id string) (*wallet.Wallet, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, player_id, provider_id, currency, balance, version, created_at, updated_at
		 FROM wallets WHERE id = $1`, id,
	)

	var (
		idVal       string
		playerID    string
		providerID  string
		currencyStr string
		balance     int64
		version     int64
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := row.Scan(&idVal, &playerID, &providerID, &currencyStr, &balance, &version, &createdAt, &updatedAt)
	if err == pgx.ErrNoRows {
		return nil, wallet.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan wallet: %w", err)
	}

	bal, err := money.NewMoney(balance, money.Currency(currencyStr))
	if err != nil {
		return nil, fmt.Errorf("parse wallet balance: %w", err)
	}

	return wallet.RehydrateWallet(idVal, playerID, providerID, money.Currency(currencyStr), bal, version, createdAt, updatedAt)
}

func (r *WalletRepository) FindByPlayerAndCurrencyAndProvider(ctx context.Context, playerID string, currency money.Currency, providerID string) (*wallet.Wallet, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, player_id, provider_id, currency, balance, version, created_at, updated_at
		 FROM wallets WHERE player_id = $1 AND currency = $2 AND provider_id = $3`, playerID, string(currency), providerID,
	)

	var (
		idVal       string
		pid         string
		provider    string
		currencyStr string
		balance     int64
		version     int64
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := row.Scan(&idVal, &pid, &provider, &currencyStr, &balance, &version, &createdAt, &updatedAt)
	if err == pgx.ErrNoRows {
		return nil, wallet.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan wallet: %w", err)
	}

	bal, err := money.NewMoney(balance, money.Currency(currencyStr))
	if err != nil {
		return nil, fmt.Errorf("parse wallet balance: %w", err)
	}

	return wallet.RehydrateWallet(idVal, pid, provider, money.Currency(currencyStr), bal, version, createdAt, updatedAt)
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, id string, newBalance money.Money, expectedVersion int64) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE wallets
		 SET balance = $1, version = version + 1, updated_at = now()
		 WHERE id = $2 AND version = $3`,
		newBalance.Amount(), id, expectedVersion,
	)
	if err != nil {
		return 0, fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, wallet.ErrConcurrentUpdate
	}
	return expectedVersion + 1, nil
}
