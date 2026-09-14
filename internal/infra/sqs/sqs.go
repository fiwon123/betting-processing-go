package sqs

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
)

type Client struct {
	client   *awssqs.Client
	queueURL string
}

func New(c cfg.Config) (*Client, error) {
	if strings.TrimSpace(c.SQS.QueueURL) == "" {
		return nil, fmt.Errorf("SQS_QUEUE_URL is required")
	}

	if strings.TrimSpace(c.AWS.Region) == "" {
		return nil, fmt.Errorf("AWS_REGION is required")
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(c.AWS.Region),
	}

	if c.AWS.Endpoint != "" {
		accessKey := cfg.GetEnv("AWS_ACCESS_KEY_ID", "test")
		secretKey := cfg.GetEnv("AWS_SECRET_ACCESS_KEY", "test")

		loadOptions = append(
			loadOptions,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					accessKey,
					secretKey,
					"",
				),
			),
		)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		context.Background(),
		loadOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	clientOptions := []func(*awssqs.Options){}

	if c.AWS.Endpoint != "" {
		clientOptions = append(clientOptions, func(options *awssqs.Options) {
			options.BaseEndpoint = aws.String(c.AWS.Endpoint)
		})
	}

	return &Client{
		client:   awssqs.NewFromConfig(awsCfg, clientOptions...),
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

func (c *Client) ReceiveMessage(
	ctx context.Context,
	params *awssqs.ReceiveMessageInput,
	optFns ...func(*awssqs.Options),
) (*awssqs.ReceiveMessageOutput, error) {
	return c.client.ReceiveMessage(ctx, params, optFns...)
}

func (c *Client) DeleteMessage(
	ctx context.Context,
	params *awssqs.DeleteMessageInput,
	optFns ...func(*awssqs.Options),
) (*awssqs.DeleteMessageOutput, error) {
	return c.client.DeleteMessage(ctx, params, optFns...)
}

func (c *Client) QueueURL() string {
	return c.queueURL
}

func (c *Client) GetQueueURL(
	ctx context.Context,
	queueName string,
) (string, error) {
	output, err := c.client.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", fmt.Errorf("get queue URL for %q: %w", queueName, err)
	}

	if output.QueueUrl == nil || *output.QueueUrl == "" {
		return "", fmt.Errorf("queue URL for %q was empty", queueName)
	}

	return *output.QueueUrl, nil
}

func (c *Client) SendMessage(
	ctx context.Context,
	queueURL string,
	body string,
	messageGroupID string,
) error {
	if strings.TrimSpace(queueURL) == "" {
		return fmt.Errorf("queue URL is required")
	}

	if body == "" {
		return fmt.Errorf("message body is required")
	}

	// FIFO queues require MessageGroupId.
	if strings.HasSuffix(queueURL, ".fifo") &&
		strings.TrimSpace(messageGroupID) == "" {
		return fmt.Errorf("message group ID is required for FIFO queues")
	}

	input := &awssqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(body),
	}

	if messageGroupID != "" {
		input.MessageGroupId = aws.String(messageGroupID)
	}

	_, err := c.client.SendMessage(ctx, input)
	if err != nil {
		return fmt.Errorf("send SQS message: %w", err)
	}

	return nil
}
