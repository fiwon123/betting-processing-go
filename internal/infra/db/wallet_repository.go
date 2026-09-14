package db

import (
	"context"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/domain"
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

func (r *WalletRepository) FindByIDForUpdate(ctx context.Context, dbTx domain.DBTx, id string) (*wallet.Wallet, error) {
	row := dbTx.QueryRow(ctx,
		`SELECT id, player_id, provider_id, currency, balance, version, created_at, updated_at
		 FROM wallets WHERE id = $1
		 FOR UPDATE`, id,
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
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, wallet.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet for update: %w", err)
	}

	bal, err := money.NewMoney(balance, money.Currency(currencyStr))
	if err != nil {
		return nil, fmt.Errorf("parse wallet balance: %w", err)
	}

	return wallet.RehydrateWallet(idVal, playerID, providerID, money.Currency(currencyStr), bal, version, createdAt, updatedAt)
}

func (r *WalletRepository) UpdateBalanceTx(ctx context.Context, dbTx domain.DBTx, id string, newBalance money.Money, expectedVersion int64) (int64, error) {
	tag, err := dbTx.Exec(ctx,
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

func (r *WalletRepository) CreateWalletTx(ctx context.Context, dbTx domain.DBTx, playerID string, providerID string, currency money.Currency, balance money.Money) (string, error) {
	var walletID string
	err := dbTx.QueryRow(ctx,
		`
		INSERT INTO wallets (
			player_id, provider_id, currency, balance, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 1, now(), now())
		RETURNING id
		`,
		playerID, providerID, string(currency), balance.Amount(),
	).Scan(&walletID)
	if err != nil {
		return "", fmt.Errorf("insert wallet: %w", err)
	}
	return walletID, nil
}

func (r *WalletRepository) CreateOpeningTx(ctx context.Context, dbTx domain.DBTx, walletID string, playerID string, amount money.Money, currency money.Currency) (string, error) {
	var txID string
	err := dbTx.QueryRow(ctx,
		`
		INSERT INTO wager_transactions (
			origin, wallet_id, player_id, transaction_type, amount, currency,
			status, created_at, updated_at, processed_at
		)
		VALUES (
			'INTERNAL', $1, $2, 'OPENING', $3, $4,
			'PROCESSED', now(), now(), now()
		)
		RETURNING id
		`,
		walletID, playerID, amount.Amount(), string(currency),
	).Scan(&txID)
	if err != nil {
		return "", fmt.Errorf("insert opening transaction: %w", err)
	}
	return txID, nil
}

func (r *WalletRepository) CreateLedgerEntryTx(ctx context.Context, dbTx domain.DBTx, entry *wallet.LedgerEntry) (string, error) {
	var entryID string
	err := dbTx.QueryRow(ctx,
		`
		INSERT INTO wallet_ledger_entries (
			wallet_id,
			transaction_id,
			direction,
			amount,
			currency,
			balance_before,
			balance_after,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		RETURNING id
		`,
		entry.WalletID(), entry.TransactionID(),
		string(entry.Direction()), entry.Amount().Amount(), string(entry.Currency()),
		entry.BalanceBefore().Amount(), entry.BalanceAfter().Amount(),
	).Scan(&entryID)
	if err != nil {
		return "", fmt.Errorf("insert ledger entry: %w", err)
	}
	return entryID, nil
}

func (r *WalletRepository) CreateOutboxEventTx(ctx context.Context, dbTx domain.DBTx, aggregateType string, aggregateID string, eventType string, payload []byte) error {
	_, err := dbTx.Exec(ctx,
		`
		INSERT INTO outbox_events (
			aggregate_type,
			aggregate_id,
			event_type,
			payload,
			occurred_at,
			attempts,
			next_attempt_at
		)
		VALUES ($1, $2, $3, $4, now(), 0, now())
		`,
		aggregateType, aggregateID, eventType, payload,
	)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}
