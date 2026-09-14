package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

func (r *LedgerRepository) Create(ctx context.Context, entry *wallet.LedgerEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO wallet_ledger_entries
		 (id, wallet_id, transaction_id, direction, amount, currency, balance_before, balance_after, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		entry.ID(), entry.WalletID(), entry.TransactionID(),
		string(entry.Direction()), entry.Amount().Amount(), string(entry.Currency()),
		entry.BalanceBefore().Amount(), entry.BalanceAfter().Amount(),
		entry.CreatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

func (r *LedgerRepository) FindByWalletID(ctx context.Context, walletID string, cursor string, limit int) ([]*wallet.LedgerEntry, string, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error

	if cursor == "" {
		rows, err = r.pool.Query(ctx,
			`SELECT id, wallet_id, transaction_id, direction, amount, currency,
			        balance_before, balance_after, created_at
			 FROM wallet_ledger_entries
			 WHERE wallet_id = $1
			 ORDER BY created_at ASC, id ASC
			 LIMIT $2`,
			walletID, limit+1,
		)
	} else {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("invalid cursor format")
		}
		cursorTime, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return nil, "", fmt.Errorf("parse cursor time: %w", err)
		}
		cursorID := parts[1]

		rows, err = r.pool.Query(ctx,
			`SELECT id, wallet_id, transaction_id, direction, amount, currency,
			        balance_before, balance_after, created_at
			 FROM wallet_ledger_entries
			 WHERE wallet_id = $1 AND (created_at, id) > ($2::timestamptz, $3::uuid)
			 ORDER BY created_at ASC, id ASC
			 LIMIT $4`,
			walletID, cursorTime, cursorID, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("query ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []*wallet.LedgerEntry
	for rows.Next() {
		var (
			id            string
			wid           string
			tid           string
			direction     string
			amount        int64
			currencyStr   string
			balanceBefore int64
			balanceAfter  int64
			createdAt     time.Time
		)
		if err := rows.Scan(&id, &wid, &tid, &direction, &amount, &currencyStr,
			&balanceBefore, &balanceAfter, &createdAt); err != nil {
			return nil, "", fmt.Errorf("scan ledger entry: %w", err)
		}

		bal, _ := money.NewMoney(amount, money.Currency(currencyStr))
		balBefore, _ := money.NewMoney(balanceBefore, money.Currency(currencyStr))
		balAfter, _ := money.NewMoney(balanceAfter, money.Currency(currencyStr))

		entry, err := wallet.RehydrateLedgerEntry(
			id, wid, tid, wallet.Direction(direction), bal, balBefore, balAfter, createdAt,
		)
		if err != nil {
			return nil, "", fmt.Errorf("rehydrate ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}

	var nextCursor string
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[len(entries)-1]
		nextCursor = fmt.Sprintf("%s:%s", last.CreatedAt().Format(time.RFC3339Nano), last.ID())
	}

	return entries, nextCursor, nil
}

func (r *LedgerRepository) SumByWalletID(ctx context.Context, walletID string) (int64, error) {
	var sum *int64
	err := r.pool.QueryRow(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN direction = 'DEBIT' THEN amount ELSE 0 END), 0)
		 FROM wallet_ledger_entries
		 WHERE wallet_id = $1`,
		walletID,
	).Scan(&sum)
	if err != nil {
		return 0, fmt.Errorf("sum ledger entries: %w", err)
	}
	if sum == nil {
		return 0, nil
	}
	return *sum, nil
}
