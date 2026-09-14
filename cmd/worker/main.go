package main

import (
	"context"
	"errors"
	"sync"
	"time"

	workers "github.com/fiwon123/betting-processing-go/internal/adapters/workers"
	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/fiwon123/betting-processing-go/internal/infra/db"
	"github.com/fiwon123/betting-processing-go/internal/infra/logger"
	"github.com/fiwon123/betting-processing-go/internal/infra/metrics"
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
			db.NewTxManager,
			func(tm *db.TxManager) domain.DBTxFactory { return tm },
			db.NewWagerTransactionRepository,
			db.NewInboxRepository,
			db.NewOutboxRepository,
			db.NewWalletRepository,
			db.NewLedgerRepository,

			wallet.NewService,
			wagertransaction.NewService,

			func(r *db.WagerTransactionRepository) wagertransaction.Repository { return r },
			func(r *db.InboxRepository) wagertransaction.InboxRepository { return r },
			func(r *db.OutboxRepository) wagertransaction.OutboxRepository { return r },
			func(r *db.WalletRepository) wallet.Repository { return r },
			func(r *db.LedgerRepository) wallet.LedgerRepository { return r },
			func(s *wallet.Service) wagertransaction.WalletService { return s },
			metrics.New,
		),

		fx.Invoke(func(
			lc fx.Lifecycle,
			appCfg cfg.Config,
			client *sqs.Client,
			wagerSvc *wagertransaction.Service,
			outboxRepo *db.OutboxRepository,
			repo *db.WagerTransactionRepository,
			log *zap.Logger,
			m *metrics.Metrics,
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

			outboundURL := appCfg.SQS.OutboundQueueURL
			if outboundURL == "" {
				outboundURL = client.QueueURL()
			}

			outboxPublisher := workers.NewOutboxPublisher(
				outboxRepo,
				workers.SQSPublishFunc(client, outboundURL),
				log,
				m,
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

					shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 25*time.Second)
					defer shutdownCancel()

					done := make(chan struct{})

					go func() {
						wg.Wait()
						close(done)
					}()

					select {
					case <-done:
						log.Info("all workers stopped gracefully")
						return nil
					case <-shutdownCtx.Done():
						log.Warn("worker shutdown timed out, forcing exit")
						return nil
					case <-stopCtx.Done():
						return stopCtx.Err()
					}
				},
			})
		}),
	).Run()
}
