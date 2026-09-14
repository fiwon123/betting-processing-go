//go:build integration

package wagertransaction

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testTxManager struct {
	pool *pgxpool.Pool
}

type testDBTx struct {
	tx interface {
		Exec(ctx context.Context, sql string, args ...any) (interface{ RowsAffected() int64 }, error)
		QueryRow(ctx context.Context, sql string, args ...any) interface{ Scan(dest ...any) error }
		Commit(ctx context.Context) error
		Rollback(ctx context.Context) error
	}
}

func (t *testTxManager) Begin(ctx context.Context) (domain.DBTx, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	return &testDBTxWrapper{tx: tx}, nil
}

type testDBTxWrapper struct {
	tx pgx.Tx
}

func (t *testDBTxWrapper) Exec(ctx context.Context, sql string, args ...any) (domain.CommandTag, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxCommandTagWrap{tag: tag}, nil
}

func (t *testDBTxWrapper) QueryRow(ctx context.Context, sql string, args ...any) domain.Row {
	return pgxRowWrap{row: t.tx.QueryRow(ctx, sql, args...)}
}

func (t *testDBTxWrapper) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t *testDBTxWrapper) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

type pgxCommandTagWrap struct {
	tag interface{ RowsAffected() int64 }
}

func (t pgxCommandTagWrap) RowsAffected() int64 {
	return t.tag.RowsAffected()
}

type pgxRowWrap struct {
	row interface{ Scan(dest ...any) error }
}

func (r pgxRowWrap) Scan(dest ...any) error {
	return r.row.Scan(dest...)
}

func newTestTxManager(pool *pgxpool.Pool) *testTxManager {
	return &testTxManager{pool: pool}
}

type testRepo struct {
	pool *pgxpool.Pool
}

func mustMoney(amount int64, currency money.Currency) money.Money {
	m, err := money.NewMoney(amount, currency)
	if err != nil {
		panic(err)
	}
	return m
}

func (r *testRepo) FindByIdempotencyKey(ctx context.Context, provider, idempotencyKey string) (*Transaction, error) {
	var id, externalID, walletID, playerID string
	var status string
	var payloadHash string
	err := r.pool.QueryRow(ctx,
		`SELECT id, COALESCE(external_id,''), wallet_id, player_id, status, COALESCE(payload_hash,'')
		 FROM wager_transactions WHERE provider = $1 AND idempotency_key = $2 LIMIT 1`,
		provider, idempotencyKey,
	).Scan(&id, &externalID, &walletID, &playerID, &status, &payloadHash)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	return &Transaction{id: id, externalID: externalID, walletID: walletID, playerID: playerID, status: TransactionStatus(status), payloadHash: payloadHash}, nil
}

func (r *testRepo) FindByProviderAndExternalID(ctx context.Context, provider, externalID string) (*Transaction, error) {
	var (
		id                string
		origin            string
		extID             string
		prov              string
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
		resultBalance     *int64
		createdAt         time.Time
		updatedAt         time.Time
		processedAt       *time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions WHERE provider = $1 AND external_id = $2 LIMIT 1`,
		provider, externalID,
	).Scan(&id, &origin, &extID, &prov, &idempotencyKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID,
		&transactionType, &amount, &currencyStr,
		&externalReference, &internalReference,
		&status, &failureCode,
		&refAttempts,
		&resultBalance,
		&createdAt, &updatedAt, &processedAt,
	)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	bal := mustMoney(amount, money.Currency(currencyStr))
	tx := RehydrateTransaction(
		id, Origin(origin), extID, prov, idempotencyKey, payloadHash,
		walletID, playerID, roundID, gameID,
		TransactionType(transactionType), bal,
		externalReference, internalReference,
		TransactionStatus(status), failureCode,
		refAttempts, nil,
		createdAt, updatedAt, processedAt,
	)
	return tx, nil
}

func (r *testRepo) FindByID(ctx context.Context, id string) (*Transaction, error) {
	var (
		origin            string
		extID             string
		prov              string
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
	err := r.pool.QueryRow(ctx,
		`SELECT origin, COALESCE(external_id,''), COALESCE(provider,''),
		        COALESCE(idempotency_key,''), COALESCE(payload_hash,''),
		        wallet_id, player_id, COALESCE(round_id,''), COALESCE(game_id,''),
		        transaction_type, amount, currency,
		        COALESCE(external_reference,''), COALESCE(internal_reference,''),
		        status, COALESCE(failure_code,''),
		        ref_attempts, ref_next_attempt_at,
		        result_balance,
		        created_at, updated_at, processed_at
		 FROM wager_transactions WHERE id = $1`, id,
	).Scan(&origin, &extID, &prov, &idempotencyKey, &payloadHash,
		&walletID, &playerID, &roundID, &gameID,
		&transactionType, &amount, &currencyStr,
		&externalReference, &internalReference,
		&status, &failureCode,
		&refAttempts, &refNextAttemptAt,
		&resultBalance,
		&createdAt, &updatedAt, &processedAt,
	)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	bal := mustMoney(amount, money.Currency(currencyStr))
	tx := RehydrateTransaction(
		id, Origin(origin), extID, prov, idempotencyKey, payloadHash,
		walletID, playerID, roundID, gameID,
		TransactionType(transactionType), bal,
		externalReference, internalReference,
		TransactionStatus(status), failureCode,
		refAttempts, refNextAttemptAt,
		createdAt, updatedAt, processedAt,
	)
	if resultBalance != nil {
		rb := mustMoney(*resultBalance, money.Currency(currencyStr))
		tx.SetResultBalance(rb)
	}
	return tx, nil
}

func (r *testRepo) UpdateStatus(ctx context.Context, id string, status TransactionStatus, failureCode string) error {
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
	return err
}
func (r *testRepo) SetInternalReference(ctx context.Context, id string, internalRef string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE wager_transactions SET internal_reference = $1, updated_at = now() WHERE id = $2`,
		internalRef, id,
	)
	return err
}

func (r *testRepo) FindPending(_ context.Context, _ int) ([]*Transaction, error) {
	return nil, nil
}

func (r *testRepo) FindPendingReferences(ctx context.Context, limit int) ([]*Transaction, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, COALESCE(external_id,''), COALESCE(provider,''),
		        wallet_id, player_id, status
		 FROM wager_transactions
		 WHERE status = 'PENDING_REFERENCE'
		   AND (ref_next_attempt_at IS NULL OR ref_next_attempt_at <= now())
		 ORDER BY created_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txs []*Transaction
	for rows.Next() {
		var (
			id       string
			extID    string
			prov     string
			walletID string
			playerID string
			status   string
		)
		if err := rows.Scan(&id, &extID, &prov, &walletID, &playerID, &status); err != nil {
			return nil, err
		}
		txs = append(txs, &Transaction{id: id, externalID: extID, walletID: walletID, playerID: playerID, provider: prov, status: TransactionStatus(status)})
	}
	return txs, nil
}

func (r *testRepo) HasSuccessfulReversal(ctx context.Context, referenceID string) (bool, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM wager_transactions
		 WHERE internal_reference = $1
		   AND transaction_type IN ('REFUND', 'ROLLBACK')
		   AND status = 'PROCESSED'`,
		referenceID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *testRepo) IncrementRefAttempts(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE wager_transactions
		 SET ref_attempts = ref_attempts + 1,
		     ref_next_attempt_at = now() + ((2 ^ least(ref_attempts + 1, 6)) || ' seconds')::interval,
		     updated_at = now()
		 WHERE id = $1`, id,
	)
	return err
}

func (r *testRepo) ExternalTransactionExists(ctx context.Context, provider, externalID, excludeIdempotencyKey string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM wager_transactions
			WHERE provider = $1 AND external_id = $2
			  AND (idempotency_key IS DISTINCT FROM $3)
		)`, provider, externalID, nullString(excludeIdempotencyKey),
	).Scan(&exists)
	return exists, err
}

func (r *testRepo) CreateTransactionTx(ctx context.Context, dbTx domain.DBTx, t *Transaction) (string, error) {
	var txID string
	err := dbTx.QueryRow(ctx,
		`INSERT INTO wager_transactions
		 (origin, external_id, provider, idempotency_key, payload_hash,
		  wallet_id, player_id, round_id, game_id,
		  transaction_type, amount, currency,
		  external_reference, internal_reference,
		  status, failure_code, result_balance, created_at, updated_at, processed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		 RETURNING id`,
		string(t.Origin()), t.ExternalID(), t.Provider(),
		t.IdempotencyKey(), t.PayloadHash(),
		t.WalletID(), t.PlayerID(), t.RoundID(), t.GameID(),
		string(t.TransactionType()), t.Amount().Amount(), string(t.Amount().Currency()),
		t.ExternalReference(), t.InternalReference(),
		string(t.Status()), t.FailureCode(), nil, t.CreatedAt(), t.UpdatedAt(), t.ProcessedAt(),
	).Scan(&txID)
	if err != nil {
		return "", err
	}
	return txID, nil
}

func (r *testRepo) UpdateStatusTx(ctx context.Context, dbTx domain.DBTx, id string, status TransactionStatus, failureCode string) error {
	var fc *string
	if failureCode != "" {
		fc = &failureCode
	}
	_, err := dbTx.Exec(ctx,
		`UPDATE wager_transactions
		 SET status = $1, failure_code = $2, updated_at = now()
		 WHERE id = $3`,
		string(status), fc, id,
	)
	return err
}

func (r *testRepo) CreateOutboxEventTx(ctx context.Context, dbTx domain.DBTx, aggregateType string, aggregateID string, eventType string, payload []byte) error {
	_, err := dbTx.Exec(ctx,
		`INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, occurred_at, attempts, next_attempt_at)
		 VALUES ($1, $2, $3, $4, now(), 0, now())`,
		aggregateType, aggregateID, eventType, payload,
	)
	return err
}

func (r *testRepo) CreateWalletLedgerEntryTx(ctx context.Context, dbTx domain.DBTx, walletID string, transactionID string, direction string, amount int64, currency string, balanceBefore int64, balanceAfter int64) (string, error) {
	var entryID string
	err := dbTx.QueryRow(ctx,
		`INSERT INTO wallet_ledger_entries
		 (wallet_id, transaction_id, direction, amount, currency, balance_before, balance_after, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 RETURNING id`,
		walletID, transactionID, direction, amount, currency, balanceBefore, balanceAfter,
	).Scan(&entryID)
	if err != nil {
		return "", err
	}
	return entryID, nil
}

func newTestRepo(pool *pgxpool.Pool) *testRepo {
	return &testRepo{pool: pool}
}

type testWalletSvc struct {
	pool *pgxpool.Pool
	t   *testing.T
}

func (w *testWalletSvc) Debit(ctx context.Context, dbTx domain.DBTx, walletID string, amount money.Money) (money.Money, money.Money, int64, error) {
	var balance int64
	var version int64
	err := dbTx.QueryRow(ctx,
		`SELECT balance, version FROM wallets WHERE id = $1 FOR UPDATE`, walletID,
	).Scan(&balance, &version)
	if err != nil {
		if w.t != nil {
			w.t.Logf("[Debit] wallet=%s SELECT error: %v", walletID, err)
		}
		return money.Money{}, money.Money{}, 0, err
	}
	if balance < amount.Amount() {
		if w.t != nil {
			w.t.Logf("[Debit] wallet=%s INSUFFICIENT balance=%d amount=%d", walletID, balance, amount.Amount())
		}
		return money.Money{}, money.Money{}, 0, ErrInsufficientBalance
	}
	newBalance := balance - amount.Amount()
	_, err = dbTx.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = version + 1 WHERE id = $2 AND version = $3`,
		newBalance, walletID, version,
	)
	if err != nil {
		if w.t != nil {
			w.t.Logf("[Debit] wallet=%s UPDATE error: %v", walletID, err)
		}
		return money.Money{}, money.Money{}, 0, err
	}
	balBefore := mustMoney(balance, amount.Currency())
	balAfter := mustMoney(newBalance, amount.Currency())
	if w.t != nil {
		w.t.Logf("[Debit] wallet=%s OK balance=%d->%d version=%d->%d", walletID, balance, newBalance, version, version+1)
	}
	return balBefore, balAfter, version + 1, nil
}

func (w *testWalletSvc) Credit(ctx context.Context, dbTx domain.DBTx, walletID string, amount money.Money) (money.Money, money.Money, int64, error) {
	var balance int64
	var version int64
	err := dbTx.QueryRow(ctx,
		`SELECT balance, version FROM wallets WHERE id = $1 FOR UPDATE`, walletID,
	).Scan(&balance, &version)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	newBalance := balance + amount.Amount()
	_, err = dbTx.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = version + 1 WHERE id = $2 AND version = $3`,
		newBalance, walletID, version,
	)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	balBefore := mustMoney(balance, amount.Currency())
	balAfter := mustMoney(newBalance, amount.Currency())
	return balBefore, balAfter, version + 1, nil
}

func (w *testWalletSvc) GetBalance(ctx context.Context, walletID string) (money.Money, error) {
	var balance int64
	var currency string
	err := w.pool.QueryRow(ctx,
		`SELECT balance, currency FROM wallets WHERE id = $1`, walletID,
	).Scan(&balance, &currency)
	if err != nil {
		return money.Money{}, err
	}
	return mustMoney(balance, money.Currency(currency)), nil
}

func newTestWalletSvc(pool *pgxpool.Pool, t *testing.T) *testWalletSvc {
	return &testWalletSvc{pool: pool, t: t}
}

type testInboxRepo struct{}

func (r *testInboxRepo) RecordReceived(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *testInboxRepo) RecordReceivedTx(_ context.Context, _ domain.DBTx, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *testInboxRepo) MarkCompleted(_ context.Context, _, _ string) error { return nil }
func (r *testInboxRepo) MarkCompletedTx(_ context.Context, _ domain.DBTx, _, _ string) error {
	return nil
}

func newTestInboxRepo(_ *pgxpool.Pool) *testInboxRepo {
	return &testInboxRepo{}
}

type testOutboxRepo struct {
	pool *pgxpool.Pool
}

func (r *testOutboxRepo) CreateEvents(ctx context.Context, events []OutboxEvent) error {
	for _, e := range events {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, occurred_at, attempts, next_attempt_at)
			 VALUES ($1, $2, $3, $4, now(), 0, now())`,
			e.AggregateType, e.AggregateID, e.EventType, e.Payload,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *testOutboxRepo) FindPending(ctx context.Context, limit int) ([]*OutboxEvent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, aggregate_type, aggregate_id, event_type, payload, attempts
		 FROM outbox_events WHERE published_at IS NULL AND next_attempt_at <= now() ORDER BY next_attempt_at ASC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*OutboxEvent
	for rows.Next() {
		e := &OutboxEvent{}
		if err := rows.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &e.Payload, &e.Attempts); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (r *testOutboxRepo) MarkPublished(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET published_at = now() WHERE id = $1`, id,
	)
	return err
}

func (r *testOutboxRepo) IncrementAttempts(ctx context.Context, id string, lastError string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET attempts = attempts + 1, last_error = $2, updated_at = now() WHERE id = $1`,
		id, lastError,
	)
	return err
}

func newTestOutboxRepo(pool *pgxpool.Pool) *testOutboxRepo {
	return &testOutboxRepo{pool: pool}
}
