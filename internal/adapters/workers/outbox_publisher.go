package workers

import (
	"context"
	"errors"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/infra/metrics"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"go.uber.org/zap"
)

type OutboxPublisher struct {
	repo         wagertransaction.OutboxRepository
	publishFunc  func(ctx context.Context, eventType, aggregateType, aggregateID string, payload []byte) error
	maxAttempts  int
	pollInterval time.Duration
	log          *zap.Logger
	metrics      *metrics.Metrics
}

func NewOutboxPublisher(
	repo wagertransaction.OutboxRepository,
	publishFunc func(
		ctx context.Context,
		eventType string,
		aggregateType string,
		aggregateID string,
		payload []byte,
	) error,
	log *zap.Logger,
	m *metrics.Metrics,
) *OutboxPublisher {
	return &OutboxPublisher{
		repo:         repo,
		publishFunc:  publishFunc,
		maxAttempts:  5,
		pollInterval: 2 * time.Second,
		log:          log,
		metrics:      m,
	}
}

func (w *OutboxPublisher) Start(ctx context.Context) error {
	w.log.Info("outbox publisher started")

	for {
		if err := ctx.Err(); err != nil {
			w.log.Info("outbox publisher stopping")
			return nil
		}

		events, err := w.repo.FindPending(ctx, 10)
		if err != nil {
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return nil
			}

			w.log.Error(
				"failed to find pending outbox events",
				zap.Error(err),
			)

			if err := sleepContext(ctx, w.pollInterval); err != nil {
				return nil
			}

			continue
		}

		if w.metrics != nil {
			w.metrics.OutboxPendingCount.Set(float64(len(events)))
		}

		for _, event := range events {
			if err := ctx.Err(); err != nil {
				return nil
			}

			w.publish(ctx, event)
		}

		if err := sleepContext(ctx, w.pollInterval); err != nil {
			return nil
		}
	}
}

func (w *OutboxPublisher) publish(
	ctx context.Context,
	event *wagertransaction.OutboxEvent,
) {
	if event == nil {
		return
	}

	if event.Attempts >= w.maxAttempts {
		w.log.Warn(
			"outbox event exceeded maximum attempts",
			zap.String("event_id", event.ID),
			zap.Int("attempts", event.Attempts),
		)
		if w.metrics != nil {
			w.metrics.DLQTotal.Inc()
		}
		return
	}

	if err := ctx.Err(); err != nil {
		return
	}

	publishStart := time.Now()
	err := w.publishFunc(
		ctx,
		event.EventType,
		event.AggregateType,
		event.AggregateID,
		event.Payload,
	)
	if w.metrics != nil {
		w.metrics.OutboxPublishLatency.Observe(time.Since(publishStart).Seconds())
	}
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return
		}

		w.log.Error(
			"failed to publish outbox event",
			zap.String("event_id", event.ID),
			zap.Error(err),
		)

		if incrementErr := w.repo.IncrementAttempts(
			ctx,
			event.ID,
			err.Error(),
		); incrementErr != nil {
			w.log.Error(
				"failed to increment outbox attempts",
				zap.String("event_id", event.ID),
				zap.Error(incrementErr),
			)
		}

		return
	}

	if err := w.repo.MarkPublished(ctx, event.ID); err != nil {
		w.log.Error(
			"failed to mark outbox event as published",
			zap.String("event_id", event.ID),
			zap.Error(err),
		)

	}
}

func SQSPublishFunc(
	sqsClient interface {
		SendMessage(
			ctx context.Context,
			queueURL string,
			body string,
			messageGroupID string,
		) error
	},
	queueURL string,
) func(
	ctx context.Context,
	eventType string,
	aggregateType string,
	aggregateID string,
	payload []byte,
) error {
	return func(
		ctx context.Context,
		eventType string,
		aggregateType string,
		aggregateID string,
		payload []byte,
	) error {
		messageGroupID := aggregateType + "-" + aggregateID

		return sqsClient.SendMessage(
			ctx,
			queueURL,
			string(payload),
			messageGroupID,
		)
	}
}
