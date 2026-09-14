package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgxCommandTag adapts pgconn.CommandTag to domain.CommandTag.
type pgxCommandTag struct {
	tag pgconn.CommandTag
}

func (t pgxCommandTag) RowsAffected() int64 {
	return t.tag.RowsAffected()
}

// pgxRow adapts pgx.Row to domain.Row.
type pgxRow struct {
	row pgx.Row
}

func (r pgxRow) Scan(dest ...any) error {
	return r.row.Scan(dest...)
}

// pgxTx wraps pgx.Tx to satisfy domain.DBTx.
type pgxTx struct {
	tx pgx.Tx
}

func (t pgxTx) Exec(ctx context.Context, sql string, args ...any) (domain.CommandTag, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxCommandTag{tag: tag}, nil
}

func (t pgxTx) QueryRow(ctx context.Context, sql string, args ...any) domain.Row {
	return pgxRow{row: t.tx.QueryRow(ctx, sql, args...)}
}

func (t pgxTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t pgxTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

// TxManager is the infrastructure implementation of domain.DBTxFactory.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager creates a TxManager wrapping a pgxpool.Pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) Begin(ctx context.Context) (domain.DBTx, error) {
	if m.pool == nil {
		return nil, fmt.Errorf("tx manager: database pool is nil")
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	return pgxTx{tx: tx}, nil
}

// Pool returns the underlying pool for repositories that need direct access.
func (m *TxManager) Pool() *pgxpool.Pool {
	return m.pool
}

// IsUniqueViolation checks if the error is a PostgreSQL unique constraint violation (code 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
