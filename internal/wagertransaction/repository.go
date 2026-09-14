package wagertransaction

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Repository interface {
	FindByID(ctx context.Context, id string) (*Transaction, error)
	FindByProviderAndExternalID(ctx context.Context, providerID, externalID string) (*Transaction, error)
	FindByIdempotencyKey(ctx context.Context, provider, idempotencyKey string) (*Transaction, error)
	UpdateStatus(ctx context.Context, id string, status TransactionStatus, failureCode string) error
	SetInternalReference(ctx context.Context, id string, internalRef string) error
	FindPending(ctx context.Context, limit int) ([]*Transaction, error)
	FindPendingReferences(ctx context.Context, limit int) ([]*Transaction, error)
	HasSuccessfulReversal(ctx context.Context, referenceID string, reversalType TransactionType) (bool, error)
	IncrementRefAttempts(ctx context.Context, id string) error
}

type LedgerEntry interface {
	WalletID() string
	TransactionID() string
}

type InboxRepository interface {
	RecordReceived(ctx context.Context, consumerName, messageID, payloadHash string) (bool, error)
	RecordReceivedTx(ctx context.Context, dbTx pgx.Tx, consumerName, messageID, payloadHash string) (bool, error)
	MarkCompleted(ctx context.Context, consumerName, messageID string) error
	MarkCompletedTx(ctx context.Context, dbTx pgx.Tx, consumerName, messageID string) error
}

type OutboxRepository interface {
	CreateEvents(ctx context.Context, events []OutboxEvent) error
	FindPending(ctx context.Context, limit int) ([]*OutboxEvent, error)
	MarkPublished(ctx context.Context, id string) error
	IncrementAttempts(ctx context.Context, id string, lastError string) error
}

type OutboxEvent struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
	Attempts      int
}
