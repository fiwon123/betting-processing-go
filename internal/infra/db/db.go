package db

import (
	"context"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
)

func NewPool(
	lc fx.Lifecycle,
	databaseConfig cfg.DatabaseConfig,
) (*pgxpool.Pool, error) {
	if databaseConfig.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	if databaseConfig.MinConns < 0 {
		return nil, fmt.Errorf("database minimum connections cannot be negative")
	}

	if databaseConfig.MaxConns <= 0 {
		return nil, fmt.Errorf("database maximum connections must be greater than zero")
	}

	if databaseConfig.MinConns > databaseConfig.MaxConns {
		return nil, fmt.Errorf(
			"database minimum connections cannot exceed maximum connections",
		)
	}

	if databaseConfig.MaxConnLifetime < 0 {
		return nil, fmt.Errorf(
			"database maximum connection lifetime cannot be negative",
		)
	}

	if databaseConfig.MaxConnIdleTime < 0 {
		return nil, fmt.Errorf(
			"database maximum connection idle time cannot be negative",
		)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseConfig.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	poolConfig.MinConns = databaseConfig.MinConns
	poolConfig.MaxConns = databaseConfig.MaxConns
	poolConfig.MaxConnLifetime = databaseConfig.MaxConnLifetime
	poolConfig.MaxConnIdleTime = databaseConfig.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			if err := pool.Ping(pingCtx); err != nil {
				return fmt.Errorf("ping database: %w", err)
			}

			return nil
		},
		OnStop: func(ctx context.Context) error {
			pool.Close()
			return nil
		},
	})

	return pool, nil
}
