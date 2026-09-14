package workers

import (
	"context"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
)

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func buildIdempotencyKey(req wagertransaction.Request) string {
	if req.ProviderID == "" {
		return req.ExternalTransactionID
	}

	return req.ProviderID + ":" + req.ExternalTransactionID
}
