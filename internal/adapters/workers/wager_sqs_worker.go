package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/fiwon123/betting-processing-go/internal/infra/sqs"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"go.uber.org/zap"
)

type JobEnvelope struct {
	MessageID  string                   `json:"messageId"`
	Type       string                   `json:"type"`
	OccurredAt string                   `json:"occurredAt"`
	Data       wagertransaction.Request `json:"data"`
}

type WagerSQSWorker struct {
	client       *sqs.Client
	service      WagerTransactionService
	queueURL     string
	pollInterval time.Duration
	maxReceive   int32
	log          *zap.Logger
}

type WagerTransactionService interface {
	ProcessTransaction(
		ctx context.Context,
		req wagertransaction.Request,
		idempotencyKey string,
		messageID string,
	) (*wagertransaction.ProcessResult, error)
}

func NewWagerSQSWorker(
	client *sqs.Client,
	service WagerTransactionService,
	log *zap.Logger,
) *WagerSQSWorker {
	return &WagerSQSWorker{
		client:       client,
		service:      service,
		queueURL:     client.QueueURL(),
		pollInterval: 2 * time.Second,
		maxReceive:   10,
		log:          log,
	}
}

func (w *WagerSQSWorker) Start(ctx context.Context) error {
	w.log.Info(
		"SQS worker started",
		zap.String("queue_url", w.queueURL),
	)

	for {
		if err := ctx.Err(); err != nil {
			w.log.Info("SQS worker stopping")
			return nil
		}

		output, err := w.client.ReceiveMessage(
			ctx,
			&awssqs.ReceiveMessageInput{
				QueueUrl:            awsString(w.queueURL),
				MaxNumberOfMessages: w.maxReceive,
				WaitTimeSeconds:     5,
				VisibilityTimeout:   30,
			},
		)
		if err != nil {
			if errors.Is(err, context.Canceled) ||
				errors.Is(err, context.DeadlineExceeded) {
				return nil
			}

			w.log.Error("SQS receive error", zap.Error(err))

			if err := sleepContext(ctx, w.pollInterval); err != nil {
				return nil
			}

			continue
		}

		for _, message := range output.Messages {
			if err := ctx.Err(); err != nil {
				return nil
			}

			w.processMessage(ctx, message)
		}

		if err := sleepContext(ctx, w.pollInterval); err != nil {
			return nil
		}
	}
}

func (w *WagerSQSWorker) processMessage(
	ctx context.Context,
	message sqstypes.Message,
) {
	body := awsStringValue(message.Body)

	var envelope JobEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		w.log.Error(
			"failed to decode SQS message",
			zap.Error(err),
		)

		if err := w.deleteMessage(ctx, message.ReceiptHandle); err != nil {
			w.log.Error(
				"failed to delete invalid SQS message",
				zap.Error(err),
			)
		}

		return
	}

	messageID := awsStringValue(message.MessageId)
	if messageID == "" {
		messageID = envelope.MessageID
	}

	log := w.log.With(
		zap.String("correlation_id", messageID),
		zap.String("message_id", messageID),
		zap.String("provider_id", envelope.Data.ProviderID),
		zap.String("wallet_id", envelope.Data.WalletID),
	)

	key := envelope.Data.IdempotencyKey
	if key == "" {
		key = buildIdempotencyKey(envelope.Data)
	}

	result, err := w.service.ProcessTransaction(
		ctx,
		envelope.Data,
		key,
		messageID,
	)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return
		}

		log.Error(
			"service rejected SQS message",
			zap.Error(err),
		)

		if wagertransaction.IsTerminalBusinessError(err) {
			if delErr := w.deleteMessage(ctx, message.ReceiptHandle); delErr != nil {
				log.Error(
					"failed to delete terminal SQS message",
					zap.Error(delErr),
				)
			}
		}

		return
	}

	if result != nil {
		log.Info(
			"SQS message processed",
			zap.String("transaction_id", result.TransactionID),
			zap.String("status", result.Status),
		)
	}

	if err := w.deleteMessage(ctx, message.ReceiptHandle); err != nil {
		log.Error(
			"failed to delete SQS message",
			zap.Error(err),
		)
	}
}

func (w *WagerSQSWorker) deleteMessage(
	ctx context.Context,
	receiptHandle *string,
) error {
	_, err := w.client.DeleteMessage(
		ctx,
		&awssqs.DeleteMessageInput{
			QueueUrl:      awsString(w.queueURL),
			ReceiptHandle: receiptHandle,
		},
	)

	return err
}

func (w *WagerSQSWorker) Process(
	ctx context.Context,
	body string,
) error {
	var envelope JobEnvelope

	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return errors.New("invalid envelope")
	}

	key := envelope.Data.IdempotencyKey
	if key == "" {
		key = buildIdempotencyKey(envelope.Data)
	}

	_, err := w.service.ProcessTransaction(
		ctx,
		envelope.Data,
		key,
		envelope.MessageID,
	)
	if err != nil {
		return fmt.Errorf("process request: %w", err)
	}

	return nil
}

func awsString(value string) *string {
	return &value
}

func awsStringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func isFIFOQueue(queueURL string) bool {
	return strings.HasSuffix(queueURL, ".fifo")
}
