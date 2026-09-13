package sqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
)

type Client struct {
	client   *awssqs.Client
	queueURL string
}

func New(c cfg.Config) (*Client, error) {
	if c.SQS.QueueURL == "" {
		return nil, fmt.Errorf("SQS_QUEUE_URL is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		awsconfig.WithRegion(c.AWS.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	sqsOptions := []func(*awssqs.Options){}

	if c.AWS.Endpoint != "" {
		sqsOptions = append(sqsOptions, func(options *awssqs.Options) {
			options.BaseEndpoint = aws.String(c.AWS.Endpoint)
		})
	}

	return &Client{
		client:   awssqs.NewFromConfig(awsCfg, sqsOptions...),
		queueURL: c.SQS.QueueURL,
	}, nil
}

func (c *Client) GetQueueAttributes(
	ctx context.Context,
	params *awssqs.GetQueueAttributesInput,
	optFns ...func(*awssqs.Options),
) (*awssqs.GetQueueAttributesOutput, error) {
	return c.client.GetQueueAttributes(ctx, params, optFns...)
}
