package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ProviderID string

type contextKey string

const ProviderIDKey contextKey = "provider_id"

const (
	defaultAlgorithm       = "RS256"
	defaultCacheTTL        = 5 * time.Minute
	refreshCooldown        = 30 * time.Second
	maxJWKSResponseSize    = 1 << 20 // 1 MiB
	minRSAModulusBitLength = 2048
)

type OIDCConfig struct {
	IssuerURL string
	ClientID  string
	JWKSURL   string
	Algorithm string
	CacheTTL time.Duration
}

type JWTClaims struct {
	jwt.RegisteredClaims
	ProviderID string `json:"provider_id"`
}

type KeyValidator struct {
	jwksURL     string
	keys        map[string]*rsa.PublicKey
	cacheExpiry time.Time
	lastRefresh time.Time
	cacheTTL    time.Duration
	expectedAlg string

	mu         sync.Mutex
	httpClient *http.Client
}

func NewKeyValidator(cfg OIDCConfig) *KeyValidator {
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}

	algorithm := cfg.Algorithm
	if algorithm == "" {
		algorithm = defaultAlgorithm
	}

	return &KeyValidator{
		jwksURL:     cfg.JWKSURL,
		keys:        make(map[string]*rsa.PublicKey),
		cacheTTL:    ttl,
		expectedAlg: algorithm,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (kv *KeyValidator) GetKey(token *jwt.Token) (any, error) {
	alg, ok := token.Header["alg"].(string)
	if !ok || alg != kv.expectedAlg {
		return nil, fmt.Errorf(
			"unexpected signing algorithm: got %q, expected %q",
			alg,
			kv.expectedAlg,
		)
	}

	kid, ok := token.Header["kid"].(string)
	if !ok || strings.TrimSpace(kid) == "" {
		return nil, fmt.Errorf("missing kid in token header")
	}

	now := time.Now()

	kv.mu.Lock()

	if now.Before(kv.cacheExpiry) {
		if key, exists := kv.keys[kid]; exists {
			kv.mu.Unlock()
			return key, nil
		}
	}

	cacheExpired := !now.Before(kv.cacheExpiry)
	shouldRefresh := cacheExpired ||
		kv.lastRefresh.IsZero() ||
		now.Sub(kv.lastRefresh) >= refreshCooldown

	kv.mu.Unlock()

	if shouldRefresh {
		if err := kv.refreshKeys(); err != nil {
			return nil, fmt.Errorf("refresh JWKS: %w", err)
		}
	}

	kv.mu.Lock()
	defer kv.mu.Unlock()

	key, exists := kv.keys[kid]
	if !exists {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}

	return key, nil
}

func (kv *KeyValidator) refreshKeys() error {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	now := time.Now()

	if now.Before(kv.cacheExpiry) &&
		!kv.lastRefresh.IsZero() &&
		now.Sub(kv.lastRefresh) < refreshCooldown {
		return nil
	}

	kv.lastRefresh = now

	req, err := http.NewRequest(http.MethodGet, kv.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("create JWKS request: %w", err)
	}

	resp, err := kv.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid     string `json:"kid"`
			N       string `json:"n"`
			E       string `json:"e"`
			Kty     string `json:"kty"`
			Alg     string `json:"alg"`
			Use     string `json:"use"`
			KeyOps  []string `json:"key_ops"`
		} `json:"keys"`
	}

	body := io.LimitReader(resp.Body, maxJWKSResponseSize)

	if err := json.NewDecoder(body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)

	for _, jwk := range jwks.Keys {
		if jwk.Kty != "RSA" {
			continue
		}

		if jwk.Kid == "" {
			continue
		}

		if jwk.Alg != "" && jwk.Alg != kv.expectedAlg {
			continue
		}

		if jwk.Use != "" && jwk.Use != "sig" {
			continue
		}

		pubKey, err := parseRSAPublicKey(jwk.N, jwk.E)
		if err != nil {
			continue
		}

		newKeys[jwk.Kid] = pubKey
	}

	if len(newKeys) == 0 {
		return fmt.Errorf("JWKS contained no valid RSA keys")
	}

	kv.keys = newKeys
	kv.cacheExpiry = time.Now().Add(kv.cacheTTL)

	return nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}

	if len(nBytes) == 0 {
		return nil, fmt.Errorf("empty modulus")
	}

	if len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, fmt.Errorf("invalid exponent length")
	}

	n := new(big.Int).SetBytes(nBytes)

	if n.Sign() <= 0 {
		return nil, fmt.Errorf("modulus must be positive")
	}

	if n.BitLen() < minRSAModulusBitLength {
		return nil, fmt.Errorf(
			"RSA modulus is too small: %d bits",
			n.BitLen(),
		)
	}

	e := 0
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}

	if e < 3 || e%2 == 0 {
		return nil, fmt.Errorf("invalid RSA exponent")
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

func OIDCAuth(cfg OIDCConfig) func(http.Handler) http.Handler {
	algorithm := cfg.Algorithm
	if algorithm == "" {
		algorithm = defaultAlgorithm
	}

	var configErr error

	switch {
	case strings.TrimSpace(cfg.IssuerURL) == "":
		configErr = fmt.Errorf("OIDC issuer URL is required")
	case strings.TrimSpace(cfg.ClientID) == "":
		configErr = fmt.Errorf("OIDC client ID is required")
	case strings.TrimSpace(cfg.JWKSURL) == "":
		configErr = fmt.Errorf("OIDC JWKS URL is required")
	case algorithm != jwt.SigningMethodRS256.Alg():
		configErr = fmt.Errorf(
			"unsupported signing algorithm %q; this middleware expects RS256",
			algorithm,
		)
	}

	validator := NewKeyValidator(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if configErr != nil {
				writeJSONError(
					w,
					http.StatusInternalServerError,
					"server_misconfigured",
					"OIDC authentication is misconfigured",
				)
				return
			}

			authHeader := r.Header.Get("Authorization")
			parts := strings.Fields(authHeader)

			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeJSONError(
					w,
					http.StatusUnauthorized,
					"invalid_authorization",
					"Authorization header must be Bearer token",
				)
				return
			}

			tokenStr := parts[1]
			claims := &JWTClaims{}

			token, err := jwt.ParseWithClaims(
				tokenStr,
				claims,
				func(token *jwt.Token) (any, error) {
					return validator.GetKey(token)
				},
				jwt.WithValidMethods([]string{algorithm}),
				jwt.WithIssuer(cfg.IssuerURL),
				jwt.WithAudience(cfg.ClientID),
			)

			if err != nil || token == nil || !token.Valid {
				writeJSONError(
					w,
					http.StatusUnauthorized,
					"invalid_token",
					"Token is invalid or expired",
				)
				return
			}

			if claims.ExpiresAt == nil {
				writeJSONError(
					w,
					http.StatusUnauthorized,
					"invalid_token",
					"Token expiration is required",
				)
				return
			}

			providerID := claims.ProviderID
			if providerID == "" {
				providerID = claims.Subject
			}

			if providerID == "" {
				writeJSONError(
					w,
					http.StatusUnauthorized,
					"missing_subject",
					"Token must contain provider_id or sub",
				)
				return
			}

			ctx := context.WithValue(
				r.Context(),
				ProviderIDKey,
				ProviderID(providerID),
			)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeJSONError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{
		Error:   code,
		Message: message,
	})
}

func GetProviderID(ctx context.Context) (string, bool) {
	pid, ok := ctx.Value(ProviderIDKey).(ProviderID)
	return string(pid), ok
}

func SkipAuth(next http.Handler) http.Handler {
	return next
}
