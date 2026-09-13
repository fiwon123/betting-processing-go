package cfg

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Database DatabaseConfig
	AWS      AWSConfig
	SQS      SQSConfig
}

type DatabaseConfig struct {
	URL             string
	MinConns        int32
	MaxConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type AWSConfig struct {
	Region   string
	Endpoint string
}

type SQSConfig struct {
	QueueURL string
}

func NewConfig() (Config, error) {
	databaseURL, err := buildDatabaseURL()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Database: DatabaseConfig{
			URL:             databaseURL,
			MinConns:        int32(getInt("API_DB_MIN_CONNS", 2)),
			MaxConns:        int32(getInt("API_DB_MAX_CONNS", 10)),
			MaxConnLifetime: getDuration("API_DB_MAX_CONN_LIFETIME", 30*time.Minute),
			MaxConnIdleTime: getDuration("API_DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		},
		AWS: AWSConfig{
			Region:   getEnv("AWS_REGION", "us-east-1"),
			Endpoint: os.Getenv("AWS_ENDPOINT_URL"),
		},
		SQS: SQSConfig{
			QueueURL: os.Getenv("SQS_QUEUE_URL"),
		},
	}, nil
}

func buildDatabaseURL() (string, error) {
	host := getEnv("API_DB_HOST", "localhost")
	port := getEnv("API_DB_PORT", "5432")
	user := os.Getenv("API_DB_USER")
	password := os.Getenv("API_DB_PASSWORD")
	name := os.Getenv("API_DB")
	sslMode := getEnv("API_DB_SSLMODE", "disable")

	if user == "" {
		return "", fmt.Errorf("DB_USER is required")
	}

	if password == "" {
		return "", fmt.Errorf("DB_PASSWORD is required")
	}

	if name == "" {
		return "", fmt.Errorf("DB_NAME is required")
	}

	databaseURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + name,
		RawQuery: url.Values{
			"sslmode": []string{sslMode},
		}.Encode(),
	}

	return databaseURL.String(), nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func getInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}
