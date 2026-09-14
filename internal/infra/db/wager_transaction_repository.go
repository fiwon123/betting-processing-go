package db

import (
	"context"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WagerTransactionRepository struct {
	pool *pgxpool.Pool
}

func NewWagerTransactionRepository(pool *pgxpool.Pool) *WagerTransactionRepository {
	return &WagerTransactionRepository{pool: pool}
}

func (r *WagerTransactionRepository) FindByID(ctx context.Context, id string) (*wagertransaction.Transaction, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions WHERE id = $1`, id,
	)
	return r.scanTransaction(row)
}

func (r *WagerTransactionRepository) FindByProviderAndExternalID(ctx context.Context, providerID, externalID string) (*wagertransaction.Transaction, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions
		 WHERE provider = $1 AND external_id = $2`, providerID, externalID,
	)
	return r.scanTransaction(row)
}

func (r *WagerTransactionRepository) FindByIdempotencyKey(ctx context.Context, provider, idempotencyKey string) (*wagertransaction.Transaction, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions
		 WHERE provider = $1 AND idempotency_key = $2`, provider, idempotencyKey,
	)
	return r.scanTransaction(row)
}

func (r *WagerTransactionRepository) UpdateStatus(ctx context.Context, id string, status wagertransaction.TransactionStatus, failureCode string) error {
	var fc *string
	if failureCode != "" {
		fc = &failureCode
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE wager_transactions
		 SET status = $1, failure_code = $2, updated_at = now(),
		     processed_at = CASE WHEN $1 = 'PROCESSED' THEN now() ELSE processed_at END
		 WHERE id = $3`,
		string(status), fc, id,
	)
	if err != nil {
		return fmt.Errorf("update transaction status: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) SetInternalReference(ctx context.Context, id string, internalRef string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE wager_transactions SET internal_reference = $1, updated_at = now() WHERE id = $2`,
		internalRef, id,
	)
	if err != nil {
		return fmt.Errorf("set internal reference: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) FindPending(ctx context.Context, limit int) ([]*wagertransaction.Transaction, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions
		 WHERE status = 'PENDING'
		 ORDER BY created_at ASC
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending transactions: %w", err)
	}
	defer rows.Close()
	return r.scanTransactions(rows)
}

func (r *WagerTransactionRepository) FindPendingReferences(ctx context.Context, limit int) ([]*wagertransaction.Transaction, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions
		 WHERE status = 'PENDING_REFERENCE'
		   AND (ref_next_attempt_at IS NULL OR ref_next_attempt_at <= now())
		 ORDER BY created_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending reference transactions: %w", err)
	}
	defer rows.Close()
	return r.scanTransactions(rows)
}

func (r *WagerTransactionRepository) HasSuccessfulReversal(ctx context.Context, referenceID string) (bool, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM wager_transactions
		 WHERE internal_reference = $1
		   AND transaction_type IN ('REFUND', 'ROLLBACK')
		   AND status = 'PROCESSED'`,
		referenceID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check successful reversal: %w", err)
	}
	return count > 0, nil
}

func (r *WagerTransactionRepository) IncrementRefAttempts(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE wager_transactions
		 SET ref_attempts = ref_attempts + 1,
		     ref_next_attempt_at = now() + ((2 ^ least(ref_attempts + 1, 6)) || ' seconds')::interval,
		     updated_at = now()
		 WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("increment ref attempts: %w", err)
	}
	return nil
}

func (r *WagerTransactionRepository) scanTransaction(row pgx.Row) (*wagertransaction.Transaction, error) {
	var (
		id                string
		origin            string
		externalID        string
		provider          string
		idempotencyKey    string
		payloadHash       string
		walletID          string
		playerID          string
		roundID           string
		gameID            string
		transactionType   string
		amount            int64
		currencyStr       string
		externalReference string
		internalReference string
		status            string
		failureCode       string
		refAttempts       int
		refNextAttemptAt  *time.Time
		resultBalance     *int64
		createdAt         time.Time
		updatedAt         time.Time
		processedAt       *time.Time
	)

	err := row.Scan(&id, &origin, &externalID, &provider, &idempotencyKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID,
		&transactionType, &amount, &currencyStr,
		&externalReference, &internalReference,
		&status, &failureCode,
		&refAttempts, &refNextAttemptAt,
		&resultBalance,
		&createdAt, &updatedAt, &processedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, wagertransaction.ErrTransactionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan transaction: %w", err)
	}

	bal, err := money.NewMoney(amount, money.Currency(currencyStr))
	if err != nil {
		return nil, fmt.Errorf("parse transaction amount: %w", err)
	}

	var rb *money.Money
	if resultBalance != nil {
		b, err := money.NewMoney(*resultBalance, money.Currency(currencyStr))
		if err != nil {
			return nil, fmt.Errorf("parse result balance: %w", err)
		}
		rb = &b
	}

	tx := wagertransaction.RehydrateTransaction(
		id,
		wagertransaction.Origin(origin),
		externalID, provider, idempotencyKey, payloadHash,
		walletID, playerID, roundID, gameID,
		wagertransaction.TransactionType(transactionType),
		bal,
		externalReference, internalReference,
		wagertransaction.TransactionStatus(status),
		failureCode,
		refAttempts, refNextAttemptAt,
		createdAt, updatedAt, processedAt,
	)
	if rb != nil {
		tx.SetResultBalance(*rb)
	}
	return tx, nil
}

func (r *WagerTransactionRepository) scanTransactions(rows pgx.Rows) ([]*wagertransaction.Transaction, error) {
	var txs []*wagertransaction.Transaction
	for rows.Next() {
		var (
			id                string
			origin            string
			externalID        string
			provider          string
			idempotencyKey    string
			payloadHash       string
			walletID          string
			playerID          string
			roundID           string
			gameID            string
			transactionType   string
			amount            int64
			currencyStr       string
			externalReference string
			internalReference string
			status            string
			failureCode       string
			refAttempts       int
			refNextAttemptAt  *time.Time
			resultBalance     *int64
			createdAt         time.Time
			updatedAt         time.Time
			processedAt       *time.Time
		)
		if err := rows.Scan(&id, &origin, &externalID, &provider, &idempotencyKey, &payloadHash,
			&walletID, &playerID, &roundID, &gameID,
			&transactionType, &amount, &currencyStr,
			&externalReference, &internalReference,
			&status, &failureCode,
			&refAttempts, &refNextAttemptAt,
			&resultBalance,
			&createdAt, &updatedAt, &processedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}

		bal, err := money.NewMoney(amount, money.Currency(currencyStr))
		if err != nil {
			return nil, fmt.Errorf("parse transaction amount: %w", err)
		}

		var rb *money.Money
		if resultBalance != nil {
			b, err := money.NewMoney(*resultBalance, money.Currency(currencyStr))
			if err != nil {
				return nil, fmt.Errorf("parse result balance: %w", err)
			}
			rb = &b
		}

		tx := wagertransaction.RehydrateTransaction(
			id,
			wagertransaction.Origin(origin),
			externalID, provider, idempotencyKey, payloadHash,
			walletID, playerID, roundID, gameID,
			wagertransaction.TransactionType(transactionType),
			bal,
			externalReference, internalReference,
			wagertransaction.TransactionStatus(status),
			failureCode,
			refAttempts, refNextAttemptAt,
			createdAt, updatedAt, processedAt,
		)
		if rb != nil {
			tx.SetResultBalance(*rb)
		}
		txs = append(txs, tx)
	}
	return txs, nil
}
