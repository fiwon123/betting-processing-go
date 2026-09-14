// @title Betting Processing API
// @version 1.0
// @description Backend service for processing betting transactions with distributed messaging
// @description ## Overview
// @description This service processes wager transactions (BET, WIN, LOSS, REFUND, ROLLBACK) between game providers and player wallets.
// @description It receives requests via SQS, applies business rules, atomically mutates wallet balances, and publishes domain events.
// @description ## Authentication
// @description Protected endpoints require a Bearer JWT token from Keycloak (OAuth2/OIDC).
// @description The JWT must contain a `provider_id` claim. Use `grant_type=password` to obtain tokens.
// @description ## Idempotency
// @description All wager transaction requests require an `Idempotency-Key` header (format: `provider:externalId`).
// @description Duplicate requests return the original result without reprocessing.
// @host localhost:8080
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer {token}"
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	_ "github.com/fiwon123/betting-processing-go/docs"
	"github.com/fiwon123/betting-processing-go/internal/adapters/handlers"
	"github.com/fiwon123/betting-processing-go/internal/adapters/middleware"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/fiwon123/betting-processing-go/internal/infra/db"
	"github.com/fiwon123/betting-processing-go/internal/infra/logger"
	"github.com/fiwon123/betting-processing-go/internal/infra/metrics"
	"github.com/fiwon123/betting-processing-go/internal/infra/sqs"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"
	"github.com/swaggo/swag"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

type Route interface {
	RegisterRoutes(chi.Router)
}

func NewRouter(routes []Route) chi.Router {
	r := chi.NewRouter()
	for _, route := range routes {
		route.RegisterRoutes(r)
	}
	return r
}

type authHandler struct {
	inner http.Handler
	auth  func(http.Handler) http.Handler
	log   *zap.Logger
}

func (h *authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/wagering") || strings.HasPrefix(path, "/providers/") || strings.HasPrefix(path, "/wallets") {
		h.auth(h.inner).ServeHTTP(w, r)
		return
	}
	h.inner.ServeHTTP(w, r)
}

func NewHTTPServer(lc fx.Lifecycle, router chi.Router, log *zap.Logger, cfg cfg.Config) *http.Server {
	router.Handle("/metrics", promhttp.Handler())

	if cfg.Environment == "development" {
		router.Get("/docs/*", httpSwagger.Handler(
			httpSwagger.URL("/docs/specs"),
		))
		router.Get("/docs/specs", func(w http.ResponseWriter, r *http.Request) {
			doc, err := swag.ReadDoc("swagger")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(doc))
		})
		log.Info("Swagger UI enabled at /docs/")
	}

	var handler http.Handler = router

	if cfg.OIDC.Enabled {
		oidcCfg := middleware.OIDCConfig{
			IssuerURL: cfg.OIDC.IssuerURL,
			ClientID:  cfg.OIDC.ClientID,
			JWKSURL:   cfg.OIDC.JWKSURL,
			CacheTTL:  5 * time.Minute,
		}
		authMiddleware := middleware.OIDCAuth(oidcCfg)
		handler = &authHandler{
			inner: router,
			auth:  authMiddleware,
			log:   log,
		}
		log.Info("OIDC auth middleware enabled for /wagering, /providers/* and /wallets/*")
	} else {
		log.Info("OIDC auth middleware disabled")
	}

	srv := &http.Server{
		Addr:    ":8080",
		Handler: middleware.CorrelationID(handler),
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			log.Info("Starting HTTP server", zap.String("addr", srv.Addr))
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error("HTTP server stopped", zap.Error(err))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})

	return srv
}

func AsRoute(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Route)),
		fx.ResultTags(`group:"routes"`),
	)
}

func NewQueueURL(c cfg.Config) string {
	return c.SQS.QueueURL
}

func NewDatabaseConfig(c cfg.Config) cfg.DatabaseConfig {
	return c.Database
}

func main() {
	fx.New(
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),
		fx.Provide(
			NewHTTPServer,
			fx.Annotate(
				NewRouter,
				fx.ParamTags(`group:"routes"`),
			),
			fx.Annotate(
				sqs.New,
				fx.As(new(handlers.SQSClient)),
			),
			fx.Annotate(
				NewQueueURL,
				fx.ResultTags(`name:"sqs-queue-url"`),
			),

			func(c cfg.Config) cfg.DatabaseConfig { return c.Database },
			db.NewPool,
			db.NewWalletRepository,
			db.NewLedgerRepository,
			db.NewWagerTransactionRepository,
			db.NewInboxRepository,
			db.NewOutboxRepository,

			wallet.NewService,
			wagertransaction.NewService,

			func(r *db.WalletRepository) wallet.Repository { return r },
			func(r *db.LedgerRepository) wallet.LedgerRepository { return r },
			func(r *db.WagerTransactionRepository) wagertransaction.Repository { return r },
			func(r *db.InboxRepository) wagertransaction.InboxRepository { return r },
			func(r *db.OutboxRepository) wagertransaction.OutboxRepository { return r },
			func(s *wallet.Service) handlers.WalletService { return s },
			func(s *wallet.Service) handlers.WalletLookup { return s },
			func(s *wallet.Service) wagertransaction.WalletService { return s },
			func(s *wagertransaction.Service) handlers.WagerTransactionService { return s },

			fx.Annotate(
				handlers.NewHealthHandler,
				fx.As(new(Route)),
				fx.ResultTags(`group:"routes"`),
				fx.ParamTags(
					"",
					"",
					`name:"sqs-queue-url"`,
				),
			),

			fx.Annotate(
				handlers.NewWalletHandler,
				fx.As(new(Route)),
				fx.ResultTags(`group:"routes"`),
			),
			fx.Annotate(
				handlers.NewWagerTransactionHandler,
				fx.As(new(Route)),
				fx.ResultTags(`group:"routes"`),
			),

			cfg.NewConfig,
			logger.NewLogger,
			metrics.New,
		),
		fx.Invoke(func(*http.Server) {}),
	).Run()
}
