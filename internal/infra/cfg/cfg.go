package cfg

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"time"
)

type Config struct {
	Environment string
	Database    DatabaseConfig
	AWS         AWSConfig
	SQS         SQSConfig
	OIDC        OIDCConfig
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

type OIDCConfig struct {
	Enabled   bool
	IssuerURL string
	ClientID  string
	JWKSURL   string
}

func NewConfig() (Config, error) {
	databaseURL, err := buildDatabaseURL()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Environment: GetEnv("API_ENVIRONMENT", "development"),
		Database: DatabaseConfig{
			URL:             databaseURL,
			MinConns:        int32(GetInt("API_DB_MIN_CONNS", 2)),
			MaxConns:        int32(GetInt("API_DB_MAX_CONNS", 10)),
			MaxConnLifetime: GetDuration("API_DB_MAX_CONN_LIFETIME", 30*time.Minute),
			MaxConnIdleTime: GetDuration("API_DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		},
		AWS: AWSConfig{
			Region:   GetEnv("AWS_REGION", "us-east-1"),
			Endpoint: os.Getenv("AWS_ENDPOINT_URL"),
		},
		SQS: SQSConfig{
			QueueURL: os.Getenv("SQS_QUEUE_URL"),
		},
		OIDC: OIDCConfig{
			Enabled:   GetEnv("API_OIDC_ENABLED", "false") == "true",
			IssuerURL: GetEnv("API_OIDC_ISSUER_URL", ""),
			ClientID:  GetEnv("API_OIDC_CLIENT_ID", ""),
			JWKSURL:   GetEnv("API_OIDC_JWKS_URL", ""),
		},
	}, nil
}

func buildDatabaseURL() (string, error) {
	host := GetEnv("API_DB_HOST", "localhost")
	port := GetEnv("API_DB_PORT", "5432")
	user := os.Getenv("API_DB_USER")
	password := os.Getenv("API_DB_PASSWORD")
	name := os.Getenv("API_DB")
	sslMode := GetEnv("API_DB_SSLMODE", "disable")

	if user == "" {
		return "", fmt.Errorf("API_DB_USER is required")
	}

	if password == "" {
		return "", fmt.Errorf("API_DB_PASSWORD is required")
	}

	if name == "" {
		return "", fmt.Errorf("API_DB is required")
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
