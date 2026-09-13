package sqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
)

type Client struct {
	client   *sqs.Client
	queueURL string
}

func New(ctx context.Context, cfg cfg.Config) (*Client, error) {
	if cfg.SQS.QueueURL == "" {
		return nil, fmt.Errorf("SQS_QUEUE_URL is required")
	}

	awsCfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithRegion(cfg.AWS.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	sqsOptions := []func(*sqs.Options){}

	if cfg.AWS.Endpoint != "" {
		sqsOptions = append(sqsOptions, func(options *sqs.Options) {
			options.BaseEndpoint = aws.String(cfg.AWS.Endpoint)
		})
	}

	client := sqs.NewFromConfig(awsCfg, sqsOptions...)

	return &Client{
		client:   client,
		queueURL: cfg.SQS.QueueURL,
	}, nil
}
