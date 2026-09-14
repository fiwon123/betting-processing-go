package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type TestEnv struct {
	APIBaseURL     string
	KCURL          string
	KCRealm        string
	KCClientID     string
	KCClientSecret string
	HTTPClient     *http.Client
	SQSClient      *sqs.Client
	SQSQueueURL    string
}

func NewTestEnv() *TestEnv {
	env := &TestEnv{
		APIBaseURL:     getEnv("API_BASE_URL", "http://localhost:8080"),
		KCURL:          getEnv("KC_URL", "http://localhost:8081"),
		KCRealm:        getEnv("KC_REALM", "betting"),
		KCClientID:     getEnv("KC_CLIENT_ID", "betting-api"),
		KCClientSecret: getEnv("KC_CLIENT_SECRET", "secret123"),
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
		SQSQueueURL:    getEnv("SQS_QUEUE_URL", ""),
	}

	if env.SQSQueueURL != "" {
		cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
			awsconfig.WithRegion(getEnv("AWS_REGION", "us-east-1")),
			awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				getEnv("AWS_ACCESS_KEY_ID", "test"),
				getEnv("AWS_SECRET_ACCESS_KEY", "test"),
				"",
			)),
			awsconfig.WithEndpointResolver(aws.EndpointResolverFunc(
				func(service, region string) (aws.Endpoint, error) {
					return aws.Endpoint{
						URL:           getEnv("AWS_ENDPOINT_URL", "http://localhost:4566"),
						SigningRegion: region,
					}, nil
				},
			)),
		)
		if err == nil {
			env.SQSClient = sqs.NewFromConfig(cfg)
		}
	}

	return env
}

func (e *TestEnv) SendSQSMessage(ctx context.Context, body string) (string, error) {
	if e.SQSClient == nil {
		return "", fmt.Errorf("SQS client not configured")
	}
	input := &sqs.SendMessageInput{
		QueueUrl:               aws.String(e.SQSQueueURL),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String("e2e-test"),
		MessageDeduplicationId: aws.String(fmt.Sprintf("e2e-%d", time.Now().UnixNano())),
	}
	result, err := e.SQSClient.SendMessage(ctx, input)
	if err != nil {
		return "", fmt.Errorf("send SQS message: %w", err)
	}
	return aws.ToString(result.MessageId), nil
}

func (e *TestEnv) GetToken(username, password string) (string, error) {
	url := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", e.KCURL, e.KCRealm)
	data := fmt.Sprintf("client_id=%s&client_secret=%s&grant_type=password&username=%s&password=%s",
		e.KCClientID, e.KCClientSecret, username, password)

	resp, err := e.HTTPClient.Post(url, "application/x-www-form-urlencoded", bytes.NewBufferString(data))
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get token failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	return tokenResp.AccessToken, nil
}

func (e *TestEnv) DoRequest(method, path string, token string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, e.APIBaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return e.HTTPClient.Do(req)
}

func ReadBody(resp *http.Response) map[string]interface{} {
	var result map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)
	return result
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
