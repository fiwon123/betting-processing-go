package main

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/fiwon123/betting-processing-go/internal/adapters/handlers"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"github.com/fiwon123/betting-processing-go/internal/infra/db"
	"github.com/fiwon123/betting-processing-go/internal/infra/logger"
	"github.com/go-chi/chi/v5"
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

func NewHTTPServer(
	lc fx.Lifecycle,
	router chi.Router,
	log *zap.Logger,
) *http.Server {
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}

			log.Info("Starting HTTP server", zap.String("addr", srv.Addr))

			go func() {
				if err := srv.Serve(ln); err != nil &&
					!errors.Is(err, http.ErrServerClosed) {
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
			AsRoute(handlers.NewHealthHandler),
			cfg.NewConfig,
			db.NewPool,
			logger.NewLogger,
		),
		fx.Invoke(func(*http.Server) {}),
	).Run()
}
