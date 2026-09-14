//go:build integration

package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type lifecycleRecorder struct {
	startCalled bool
	stopCalled  bool
}

func TestFxLifecycle(t *testing.T) {
	recorder := &lifecycleRecorder{}

	app := fxtest.New(t,
		fx.Supply(recorder),
		fx.Invoke(func(r *lifecycleRecorder, lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					r.startCalled = true
					return nil
				},
				OnStop: func(ctx context.Context) error {
					r.stopCalled = true
					return nil
				},
			})
		}),
	)

	app.RequireStart().RequireStop()

	if !recorder.startCalled {
		t.Error("OnStart hook was not called")
	}
	if !recorder.stopCalled {
		t.Error("OnStop hook was not called")
	}
}

func TestFxAppStartsAndStops(t *testing.T) {
	app := fxtest.New(t,
		fx.Supply(&http.Server{Addr: ":0"}),
		fx.Invoke(func(srv *http.Server) {}),
	)

	app.RequireStart().RequireStop()
}

func TestFxAppWithDB(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping DB integration test")
	}

	var pool *pgxpool.Pool

	app := fxtest.New(t,
		fx.Invoke(func(lc fx.Lifecycle) error {
			cfg, err := pgxpool.ParseConfig(dbURL)
			if err != nil {
				return err
			}

			p, err := pgxpool.NewWithConfig(context.Background(), cfg)
			if err != nil {
				return err
			}
			pool = p

			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					pool.Close()
					return nil
				},
			})

			return nil
		}),
	)

	app.RequireStart()

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		t.Fatalf("pool should be pingable before stop: %v", err)
	}

	app.RequireStop()

	pingCtx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	err := pool.Ping(pingCtx2)
	if err == nil {
		t.Error("expected pool.Ping to fail after stop")
	}
}
