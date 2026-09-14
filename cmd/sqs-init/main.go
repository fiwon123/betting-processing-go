package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
)

const (
	defaultRegion    = "us-east-1"
	defaultEndpoint  = "http://ministack:4566"
	defaultAccessKey = "test"
	defaultSecretKey = "test"
	defaultQueueName = "wager-transactions.fifo"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	region := cfg.GetEnv("AWS_REGION", defaultRegion)
	endpoint := cfg.GetEnv("AWS_ENDPOINT_URL", defaultEndpoint)
	accessKey := cfg.GetEnv("AWS_ACCESS_KEY_ID", defaultAccessKey)
	secretKey := cfg.GetEnv("AWS_SECRET_ACCESS_KEY", defaultSecretKey)

	queueName := cfg.GetEnv("SQS_QUEUE_NAME", defaultQueueName)
	outboundQueueName := cfg.GetEnv(
		"SQS_OUTBOUND_QUEUE_NAME",
		"wager-events-outbound.fifo",
	)

	if !strings.HasSuffix(queueName, ".fifo") {
		log.Fatalf("SQS_QUEUE_NAME must end with .fifo: %s", queueName)
	}

	if !strings.HasSuffix(outboundQueueName, ".fifo") {
		log.Fatalf(
			"SQS_OUTBOUND_QUEUE_NAME must end with .fifo: %s",
			outboundQueueName,
		)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				accessKey,
				secretKey,
				"",
			),
		),
	)
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}

	client := sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
		options.BaseEndpoint = aws.String(endpoint)
	})

	mainQueueURL, err := createQueue(
		ctx,
		client,
		queueName,
		map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "true",
			"VisibilityTimeout":         "30",
			"MessageRetentionPeriod":    "1209600",
		},
	)
	if err != nil {
		log.Fatalf("create main queue: %v", err)
	}

	log.Printf("SQS queue %s is ready: %s", queueName, mainQueueURL)

	dlqName := strings.TrimSuffix(queueName, ".fifo") + "-dlq.fifo"

	dlqURL, err := createQueue(
		ctx,
		client,
		dlqName,
		map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "true",
			"MessageRetentionPeriod":    "1209600",
		},
	)
	if err != nil {
		log.Fatalf("create DLQ: %v", err)
	}

	log.Printf("SQS DLQ %s is ready: %s", dlqName, dlqURL)

	outboundQueueURL, err := createQueue(
		ctx,
		client,
		outboundQueueName,
		map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "true",
			"VisibilityTimeout":         "30",
			"MessageRetentionPeriod":    "1209600",
		},
	)
	if err != nil {
		log.Fatalf("create outbound queue: %v", err)
	}

	log.Printf(
		"SQS outbound queue %s is ready: %s",
		outboundQueueName,
		outboundQueueURL,
	)

	dlqARN, err := getQueueARN(ctx, client, dlqURL)
	if err != nil {
		log.Fatalf("get DLQ ARN: %v", err)
	}

	redrivePolicy, err := json.Marshal(map[string]string{
		"deadLetterTargetArn": dlqARN,
		"maxReceiveCount":     "3",
	})
	if err != nil {
		log.Fatalf("encode redrive policy: %v", err)
	}

	_, err = client.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: aws.String(mainQueueURL),
		Attributes: map[string]string{
			"RedrivePolicy": string(redrivePolicy),
		},
	})
	if err != nil {
		log.Fatalf("set redrive policy: %v", err)
	}

	log.Printf("Redrive policy set on %s", queueName)
}

func createQueue(
	ctx context.Context,
	client *sqs.Client,
	queueName string,
	attributes map[string]string,
) (string, error) {
	output, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName:  aws.String(queueName),
		Attributes: attributes,
	})
	if err != nil {
		return "", fmt.Errorf("create queue %q: %w", queueName, err)
	}

	if output.QueueUrl == nil || *output.QueueUrl == "" {
		return "", fmt.Errorf("queue %q returned an empty URL", queueName)
	}

	return *output.QueueUrl, nil
}

func getQueueARN(
	ctx context.Context,
	client *sqs.Client,
	queueURL string,
) (string, error) {
	output, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("get queue attributes: %w", err)
	}

	queueARN := output.Attributes["QueueArn"]
	if queueARN == "" {
		return "", fmt.Errorf("QueueArn was not returned")
	}

	return queueARN, nil
}
