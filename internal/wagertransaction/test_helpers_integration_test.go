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

func (r *testRepo) FindByIdempotencyKey(_ context.Context, _, _ string) (*Transaction, error) {
	return nil, ErrTransactionNotFound
}
func (r *testRepo) FindByProviderAndExternalID(_ context.Context, _, _ string) (*Transaction, error) {
	return nil, ErrTransactionNotFound
}
func (r *testRepo) FindByID(_ context.Context, _ string) (*Transaction, error) {
	return nil, ErrTransactionNotFound
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
func (r *testRepo) HasSuccessfulReversal(_ context.Context, _ string, _ TransactionType) (bool, error) {
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

type testOutboxRepo struct{}

func (r *testOutboxRepo) CreateEvents(_ context.Context, _ []OutboxEvent) error { return nil }
func (r *testOutboxRepo) FindPending(_ context.Context, _ int) ([]*OutboxEvent, error) {
	return nil, nil
}
func (r *testOutboxRepo) MarkPublished(_ context.Context, _ string) error     { return nil }
func (r *testOutboxRepo) IncrementAttempts(_ context.Context, _ string) error { return nil }

func newTestOutboxRepo(_ *pgxpool.Pool) *testOutboxRepo {
	return &testOutboxRepo{}
}
