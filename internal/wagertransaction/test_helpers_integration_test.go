//go:build integration

package wagertransaction

import (
	"context"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testRepo struct {
	pool *pgxpool.Pool
}

func (r *testRepo) FindByIdempotencyKey(ctx context.Context, provider, idempotencyKey string) (*Transaction, error) {
	var id, externalID, walletID, playerID string
	var status string
	var payloadHash string
	err := r.pool.QueryRow(ctx,
		`SELECT id, external_transaction_id, wallet_id, player_id, status, COALESCE(payload_hash,'')
		 FROM wager_transactions WHERE provider_id = $1 AND idempotency_key = $2 LIMIT 1`,
		provider, idempotencyKey,
	).Scan(&id, &externalID, &walletID, &playerID, &status, &payloadHash)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	return &Transaction{id: id, externalID: externalID, walletID: walletID, playerID: playerID, status: TransactionStatus(status), payloadHash: payloadHash}, nil
}

func (r *testRepo) FindByProviderAndExternalID(ctx context.Context, provider, externalID string) (*Transaction, error) {
	var id, walletID, playerID string
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT id, wallet_id, player_id, status FROM wager_transactions WHERE provider_id = $1 AND external_transaction_id = $2 LIMIT 1`,
		provider, externalID,
	).Scan(&id, &walletID, &playerID, &status)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	return &Transaction{id: id, externalID: externalID, walletID: walletID, playerID: playerID, status: TransactionStatus(status)}, nil
}

func (r *testRepo) FindByID(ctx context.Context, id string) (*Transaction, error) {
	var externalID, walletID, playerID string
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT external_transaction_id, wallet_id, player_id, status FROM wager_transactions WHERE id = $1`, id,
	).Scan(&externalID, &walletID, &playerID, &status)
	if err != nil {
		return nil, ErrTransactionNotFound
	}
	return &Transaction{id: id, externalID: externalID, walletID: walletID, playerID: playerID, status: TransactionStatus(status)}, nil
}

func (r *testRepo) UpdateStatus(_ context.Context, _ string, _ TransactionStatus, _ string) error {
	return nil
}
func (r *testRepo) SetInternalReference(_ context.Context, _ string, _ string) error { return nil }

func (r *testRepo) FindPending(_ context.Context, _ int) ([]*Transaction, error) {
	return nil, nil
}

func (r *testRepo) FindPendingReferences(_ context.Context, _ int) ([]*Transaction, error) {
	return nil, nil
}

func (r *testRepo) HasSuccessfulReversal(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *testRepo) IncrementRefAttempts(_ context.Context, _ string) error { return nil }

func newTestRepo(pool *pgxpool.Pool) *testRepo {
	return &testRepo{pool: pool}
}

type testWalletSvc struct {
	pool *pgxpool.Pool
}

func (w *testWalletSvc) Debit(ctx context.Context, dbTx pgx.Tx, walletID string, amount money.Money) (money.Money, money.Money, int64, error) {
	var balance int64
	var version int64
	err := w.pool.QueryRow(ctx,
		`SELECT balance, version FROM wallets WHERE id = $1 FOR UPDATE`, walletID,
	).Scan(&balance, &version)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	if balance < amount.Amount() {
		return money.Money{}, money.Money{}, 0, ErrInsufficientBalance
	}
	newBalance := balance - amount.Amount()
	_, err = w.pool.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = version + 1 WHERE id = $2 AND version = $3`,
		newBalance, walletID, version,
	)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	balBefore := money.NewMoney(balance, amount.Currency())
	balAfter := money.NewMoney(newBalance, amount.Currency())
	return balBefore, balAfter, version + 1, nil
}

func (w *testWalletSvc) Credit(ctx context.Context, dbTx pgx.Tx, walletID string, amount money.Money) (money.Money, money.Money, int64, error) {
	var balance int64
	var version int64
	err := w.pool.QueryRow(ctx,
		`SELECT balance, version FROM wallets WHERE id = $1 FOR UPDATE`, walletID,
	).Scan(&balance, &version)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	newBalance := balance + amount.Amount()
	_, err = w.pool.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = version + 1 WHERE id = $2 AND version = $3`,
		newBalance, walletID, version,
	)
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}
	balBefore := money.NewMoney(balance, amount.Currency())
	balAfter := money.NewMoney(newBalance, amount.Currency())
	return balBefore, balAfter, version + 1, nil
}

func newTestWalletSvc(pool *pgxpool.Pool) *testWalletSvc {
	return &testWalletSvc{pool: pool}
}

type testInboxRepo struct{}

func (r *testInboxRepo) RecordReceived(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *testInboxRepo) RecordReceivedTx(_ context.Context, _ pgx.Tx, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *testInboxRepo) MarkCompleted(_ context.Context, _, _ string) error { return nil }
func (r *testInboxRepo) MarkCompletedTx(_ context.Context, _ pgx.Tx, _, _ string) error {
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
			`INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, status, attempts, last_error, created_at)
			 VALUES ($1, $2, $3, $4, $5, 'PENDING', 0, NULL, now())`,
			e.ID, e.AggregateType, e.AggregateID, e.EventType, e.Payload,
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
		 FROM outbox_events WHERE status = 'PENDING' ORDER BY created_at LIMIT $1`, limit,
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
		`UPDATE outbox_events SET status = 'PUBLISHED', published_at = now() WHERE id = $1`, id,
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
