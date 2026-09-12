package db

import (
	"context"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(cfg cfg.DatabaseConfig) (*pgxpool.Pool, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	if cfg.MinConns < 0 {
		return nil, fmt.Errorf("database minimum connections cannot be negative")
	}

	if cfg.MaxConns <= 0 {
		return nil, fmt.Errorf("database maximum connections must be greater than zero")
	}

	if cfg.MinConns > cfg.MaxConns {
		return nil, fmt.Errorf("database minimum connections cannot exceed maximum connections")
	}

	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolConfig.HealthCheckPeriod = time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
