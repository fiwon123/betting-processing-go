package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type SQSClient interface {
	GetQueueAttributes(
		ctx context.Context,
		params *sqs.GetQueueAttributesInput,
		optFns ...func(*sqs.Options),
	) (*sqs.GetQueueAttributesOutput, error)
}

type HealthHandler struct {
	db       *pgxpool.Pool
	sqs      SQSClient
	queueURL string
	log      *zap.Logger
}

func NewHealthHandler(
	db *pgxpool.Pool,
	sqsClient SQSClient,
	queueURL string,
	log *zap.Logger,
) *HealthHandler {
	return &HealthHandler{
		db:       db,
		sqs:      sqsClient,
		queueURL: queueURL,
		log:      log,
	}
}

func (h *HealthHandler) RegisterRoutes(r chi.Router) {
	r.Route("/health", func(r chi.Router) {
		r.Get("/live", h.live)
		r.Get("/ready", h.ready)
	})
}

func (h *HealthHandler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *HealthHandler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	postgresErr := h.db.Ping(ctx)
	sqsErr := h.checkSQS(ctx)

	if postgresErr != nil {
		h.log.Error("Postgres health check failed", zap.Error(postgresErr))
	}

	if sqsErr != nil {
		h.log.Error("SQS health check failed", zap.Error(sqsErr))
	}

	postgresOK := postgresErr == nil
	sqsOK := sqsErr == nil

	status := http.StatusOK
	overall := "ok"

	if !postgresOK || !sqsOK {
		status = http.StatusServiceUnavailable
		overall = "not_ready"
	}

	writeJSON(w, status, map[string]any{
		"status": overall,
		"checks": map[string]string{
			"postgres": checkStatus(postgresOK),
			"sqs":      checkStatus(sqsOK),
		},
	})
}

func (h *HealthHandler) checkSQS(ctx context.Context) error {
	if h.queueURL == "" {
		return fmt.Errorf("SQS queue URL is empty")
	}

	_, err := h.sqs.GetQueueAttributes(
		ctx,
		&sqs.GetQueueAttributesInput{
			QueueUrl: &h.queueURL,
			AttributeNames: []types.QueueAttributeName{
				types.QueueAttributeNameApproximateNumberOfMessages,
			},
		},
	)

	return err
}

func checkStatus(ok bool) string {
	if ok {
		return "up"
	}

	return "down"
}
