package db

import (
	"context"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type InboxRepository struct {
	pool *pgxpool.Pool
}

func NewInboxRepository(pool *pgxpool.Pool) *InboxRepository {
	return &InboxRepository{pool: pool}
}

func (r *InboxRepository) RecordReceived(ctx context.Context, consumerName, messageID, payloadHash string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO inbox_messages (consumer_name, message_id, payload_hash, received_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (consumer_name, message_id) DO NOTHING`,
		consumerName, messageID, payloadHash,
	)
	if err != nil {
		return false, fmt.Errorf("insert inbox message: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *InboxRepository) MarkCompleted(ctx context.Context, consumerName, messageID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE inbox_messages
		 SET completed_at = now()
		 WHERE consumer_name = $1 AND message_id = $2 AND completed_at IS NULL`,
		consumerName, messageID,
	)
	if err != nil {
		return fmt.Errorf("mark inbox completed: %w", err)
	}
	return nil
}

func (r *InboxRepository) RecordReceivedTx(ctx context.Context, dbTx pgx.Tx, consumerName, messageID, payloadHash string) (bool, error) {
	tag, err := dbTx.Exec(ctx,
		`INSERT INTO inbox_messages (consumer_name, message_id, payload_hash, received_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (consumer_name, message_id) DO NOTHING`,
		consumerName, messageID, payloadHash,
	)
	if err != nil {
		return false, fmt.Errorf("insert inbox message: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *InboxRepository) MarkCompletedTx(ctx context.Context, dbTx pgx.Tx, consumerName, messageID string) error {
	_, err := dbTx.Exec(ctx,
		`UPDATE inbox_messages
		 SET completed_at = now()
		 WHERE consumer_name = $1 AND message_id = $2 AND completed_at IS NULL`,
		consumerName, messageID,
	)
	if err != nil {
		return fmt.Errorf("mark inbox completed: %w", err)
	}
	return nil
}

type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

func (r *OutboxRepository) CreateEvents(ctx context.Context, events []wagertransaction.OutboxEvent) error {
	for _, ev := range events {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO outbox_events (id, aggregate_type, aggregate_id, event_type, payload, occurred_at, attempts, next_attempt_at)
			 VALUES ($1, $2, $3, $4, $5, now(), 0, now())`,
			ev.ID, ev.AggregateType, ev.AggregateID, ev.EventType, ev.Payload,
		)
		if err != nil {
			return fmt.Errorf("insert outbox event: %w", err)
		}
	}
	return nil
}

func (r *OutboxRepository) FindPending(ctx context.Context, limit int) ([]*wagertransaction.OutboxEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, aggregate_type, aggregate_id, event_type, payload, attempts
		 FROM outbox_events
		 WHERE published_at IS NULL AND next_attempt_at <= now()
		 ORDER BY next_attempt_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending outbox events: %w", err)
	}
	defer rows.Close()

	var events []*wagertransaction.OutboxEvent
	for rows.Next() {
		var (
			id            string
			aggregateType string
			aggregateID   string
			eventType     string
			payload       []byte
			attempts      int
		)
		if err := rows.Scan(&id, &aggregateType, &aggregateID, &eventType, &payload, &attempts); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, &wagertransaction.OutboxEvent{
			ID:            id,
			AggregateType: aggregateType,
			AggregateID:   aggregateID,
			EventType:     eventType,
			Payload:       payload,
			Attempts:      attempts,
		})
	}
	return events, nil
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET published_at = now() WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	return nil
}

func (r *OutboxRepository) IncrementAttempts(ctx context.Context, id string, lastError string) error {
	backoff := time.Duration(1<<min(r.getAttempts(ctx, id), 5)) * time.Second
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events
		 SET attempts = attempts + 1, last_error = $1, next_attempt_at = now() + $2::interval
		 WHERE id = $3`,
		lastError, fmt.Sprintf("%ds", int(backoff.Seconds())), id,
	)
	if err != nil {
		return fmt.Errorf("increment outbox attempts: %w", err)
	}
	return nil
}

func (r *OutboxRepository) getAttempts(ctx context.Context, id string) int {
	var attempts int
	_ = r.pool.QueryRow(ctx, `SELECT attempts FROM outbox_events WHERE id = $1`, id).Scan(&attempts)
	return attempts
}
