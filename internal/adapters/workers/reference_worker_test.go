package workers

import (
	"context"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type stubRefRepo struct{}

func (r *stubRefRepo) FindByID(_ context.Context, _ string) (*wagertransaction.Transaction, error) {
	return nil, wagertransaction.ErrTransactionNotFound
}

func (r *stubRefRepo) FindByProviderAndExternalID(_ context.Context, _, _ string) (*wagertransaction.Transaction, error) {
	return nil, wagertransaction.ErrTransactionNotFound
}

func (r *stubRefRepo) FindByIdempotencyKey(_ context.Context, _, _ string) (*wagertransaction.Transaction, error) {
	return nil, wagertransaction.ErrTransactionNotFound
}

func (r *stubRefRepo) UpdateStatus(_ context.Context, _ string, _ wagertransaction.TransactionStatus, _ string) error {
	return nil
}

func (r *stubRefRepo) SetInternalReference(_ context.Context, _ string, _ string) error { return nil }

func (r *stubRefRepo) FindPending(_ context.Context, _ int) ([]*wagertransaction.Transaction, error) {
	return nil, nil
}

func (r *stubRefRepo) FindPendingReferences(_ context.Context, _ int) ([]*wagertransaction.Transaction, error) {
	return nil, nil
}

func (r *stubRefRepo) HasSuccessfulReversal(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (r *stubRefRepo) IncrementRefAttempts(_ context.Context, _ string) error { return nil }

type stubInboxRepoRef struct{}

func (r *stubInboxRepoRef) RecordReceived(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *stubInboxRepoRef) RecordReceivedTx(_ context.Context, _ pgx.Tx, _, _, _ string) (bool, error) {
	return true, nil
}

func (r *stubInboxRepoRef) MarkCompleted(_ context.Context, _, _ string) error { return nil }
func (r *stubInboxRepoRef) MarkCompletedTx(_ context.Context, _ pgx.Tx, _, _ string) error {
	return nil
}

type stubOutboxRepoRef struct{}

func (r *stubOutboxRepoRef) CreateEvents(_ context.Context, _ []wagertransaction.OutboxEvent) error {
	return nil
}

func (r *stubOutboxRepoRef) FindPending(_ context.Context, _ int) ([]*wagertransaction.OutboxEvent, error) {
	return nil, nil
}

func (r *stubOutboxRepoRef) MarkPublished(_ context.Context, _ string) error { return nil }
func (r *stubOutboxRepoRef) IncrementAttempts(_ context.Context, _ string, _ string) error {
	return nil
}

func TestReferenceWorker_StartStop(t *testing.T) {
	log := zap.NewNop()

	repo := &stubRefRepo{}
	svc := wagertransaction.NewService(
		repo,
		&stubInboxRepoRef{},
		&stubOutboxRepoRef{},
		nil,
		nil,
		nil,
	)

	worker := NewReferenceWorker(repo, svc, log)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- worker.Start(ctx)
	}()

	// Let the worker run a few poll cycles
	time.Sleep(2 * time.Second)

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop within timeout")
	}

	t.Log("reference worker started and stopped cleanly")
}
