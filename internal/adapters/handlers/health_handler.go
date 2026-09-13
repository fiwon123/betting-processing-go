package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/go-chi/chi/v5"
)

type SQSClient interface {
	GetQueueAttributes(
		ctx context.Context,
		params *sqs.GetQueueAttributesInput,
		optFns ...func(*sqs.Options),
	) (*sqs.GetQueueAttributesOutput, error)
}

type HealthHandler struct {
	db       *sql.DB
	sqs      SQSClient
	queueURL string
}

func NewHealthHandler(
	db *sql.DB,
	sqsClient SQSClient,
	queueURL string,
) *HealthHandler {
	return &HealthHandler{
		db:       db,
		sqs:      sqsClient,
		queueURL: queueURL,
	}
}

func (h *HealthHandler) RegisterRoutes(r chi.Router) {
	r.Get("/health/live", h.live)
	r.Get("/health/ready", h.ready)
}

func (h *HealthHandler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *HealthHandler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	postgresOK := h.db.PingContext(ctx) == nil
	sqsOK := h.checkSQS(ctx) == nil

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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
