package domain

import "context"

// DBTx represents a database transaction that repositories can use
// to participate in atomic operations. The concrete implementation
// lives in the infrastructure layer (internal/infra/db).
type DBTx interface {
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Row is an interface for scanning a single row result.
type Row interface {
	Scan(dest ...any) error
}

// CommandTag represents the result of an Exec operation.
type CommandTag interface {
	RowsAffected() int64
}

// DBTxFactory opens new database transactions. The concrete implementation
// wraps the connection pool and lives in the infrastructure layer.
type DBTxFactory interface {
	Begin(ctx context.Context) (DBTx, error)
}
