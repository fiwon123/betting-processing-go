package main

import (
	"context"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/fiwon123/betting-processing-go/internal/infra/cfg"
)

func main() {
	region := cfg.GetEnv("AWS_REGION", "us-east-1")
	endpoint := cfg.GetEnv(
		"AWS_ENDPOINT_URL",
		"http://ministack:4566",
	)
	key := cfg.GetEnv("AWS_ACCESS_KEY_ID", "test")
	secret := cfg.GetEnv("AWS_SECRET_ACCESS_KEY", "test")
	queueName := cfg.GetEnv("SQS_QUEUE_NAME", "local-queue")

	ctx := context.Background()

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				key,
				secret,
				"",
			),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
		options.BaseEndpoint = aws.String(endpoint)
	})

	_, err = client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("SQS queue %s is ready", queueName)
}
