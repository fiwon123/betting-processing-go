package main

import (
	"context"
	"errors"
	"sync"

	workers "github.com/fiwon123/betting-processing-go/internal/adapters/workers"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/fiwon123/betting-processing-go/internal/infra/db"
	"github.com/fiwon123/betting-processing-go/internal/infra/logger"
	"github.com/fiwon123/betting-processing-go/internal/infra/sqs"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func main() {
	fx.New(
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		fx.Provide(
			cfg.NewConfig,
			func(c cfg.Config) cfg.DatabaseConfig {
				return c.Database
			},
			logger.NewLogger,
			sqs.New,
			db.NewPool,
			db.NewWagerTransactionRepository,
			db.NewInboxRepository,
			db.NewOutboxRepository,
			db.NewWalletRepository,
			db.NewLedgerRepository,
			wallet.NewService,
			wagertransaction.NewService,
		),

		fx.Invoke(func(
			lc fx.Lifecycle,
			client *sqs.Client,
			wagerSvc *wagertransaction.Service,
			outboxRepo *db.OutboxRepository,
			repo *db.WagerTransactionRepository,
			log *zap.Logger,
		) {
			ctx, cancel := context.WithCancel(context.Background())

			sqsWorker := workers.NewWagerSQSWorker(
				client,
				wagerSvc,
				log,
			)

			refWorker := workers.NewReferenceWorker(
				repo,
				wagerSvc,
				log,
			)

			outboxPublisher := workers.NewOutboxPublisher(
				outboxRepo,
				workers.SQSPublishFunc(client, client.QueueURL()),
				log,
			)

			var wg sync.WaitGroup

			run := func(name string, fn func(context.Context) error) {
				wg.Add(1)

				go func() {
					defer wg.Done()

					if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
						log.Error(
							name+" stopped unexpectedly",
							zap.Error(err),
						)
					}
				}()
			}

			lc.Append(fx.Hook{
				OnStart: func(startCtx context.Context) error {
					run("sqs worker", sqsWorker.Start)
					run("reference worker", refWorker.Start)
					run("outbox publisher", outboxPublisher.Start)

					return nil
				},

				OnStop: func(stopCtx context.Context) error {
					cancel()

					done := make(chan struct{})

					go func() {
						wg.Wait()
						close(done)
					}()

					select {
					case <-done:
						return nil
					case <-stopCtx.Done():
						return stopCtx.Err()
					}
				},
			})
		}),
	).Run()
}
