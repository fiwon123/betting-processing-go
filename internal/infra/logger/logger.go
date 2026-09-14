package logger

import (
	"context"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func NewLogger(lc fx.Lifecycle, config cfg.Config) (*zap.Logger, error) {
	var (
		log *zap.Logger
		err error
	)

	if config.Environment == "development" {
		log, err = zap.NewDevelopment()
	} else {
		log, err = zap.NewProduction()
	}

	if err != nil {
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return log.Sync()
		},
	})

	return log, nil
}
