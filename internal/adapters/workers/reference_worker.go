package workers

import (
	"context"
	"errors"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"go.uber.org/zap"
)

type ReferenceWorker struct {
	repo         wagertransaction.Repository
	service      *wagertransaction.Service
	pollInterval time.Duration
	log          *zap.Logger
}

func NewReferenceWorker(
	repo wagertransaction.Repository,
	service *wagertransaction.Service,
	log *zap.Logger,
) *ReferenceWorker {
	return &ReferenceWorker{
		repo:         repo,
		service:      service,
		pollInterval: 10 * time.Second,
		log:          log,
	}
}

func (w *ReferenceWorker) Start(ctx context.Context) error {
	w.log.Info("reference worker started")

	for {
		if err := ctx.Err(); err != nil {
			w.log.Info("reference worker stopping")
			return nil
		}

		transactions, err := w.repo.FindPendingReferences(ctx, 10)
		if err != nil {
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return nil
			}

			w.log.Error(
				"failed to find pending references",
				zap.Error(err),
			)

			if err := sleepContext(ctx, w.pollInterval); err != nil {
				return nil
			}

			continue
		}

		for _, transaction := range transactions {
			if err := ctx.Err(); err != nil {
				return nil
			}

			w.processPending(ctx, transaction)
		}

		if err := sleepContext(ctx, w.pollInterval); err != nil {
			return nil
		}
	}
}

func (w *ReferenceWorker) processPending(
	ctx context.Context,
	transaction *wagertransaction.Transaction,
) {
	if transaction == nil {
		return
	}

	result, err := w.service.ResolvePendingReference(
		ctx,
		transaction.ID(),
	)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return
		}

		w.log.Error(
			"failed to resolve pending reference",
			zap.String("transaction_id", transaction.ID()),
			zap.Error(err),
		)

		return
	}

	if result == nil {
		return
	}

	w.log.Info(
		"reference resolved",
		zap.String("transaction_id", transaction.ID()),
		zap.String("status", result.Status),
		zap.Int("ref_attempts", transaction.RefAttempts()+1),
	)
}
