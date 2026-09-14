package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProviderID(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantID   string
		wantOK   bool
	}{
		{
			name:   "returns provider_id when set",
			ctx:    context.WithValue(context.Background(), ProviderIDKey, ProviderID("prov-123")),
			wantID: "prov-123",
			wantOK: true,
		},
		{
			name:   "returns false when not set",
			ctx:    context.Background(),
			wantID: "",
			wantOK: false,
		},
		{
			name:   "returns false for wrong type in context",
			ctx:    context.WithValue(context.Background(), ProviderIDKey, "not-a-provider-id"),
			wantID: "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := GetProviderID(tt.ctx)
			if id != tt.wantID {
				t.Errorf("GetProviderID() id = %q, want %q", id, tt.wantID)
			}
			if ok != tt.wantOK {
				t.Errorf("GetProviderID() ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestSkipAuth(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := SkipAuth(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("SkipAuth did not call the next handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestOIDCAuth_MissingAuthHeader(t *testing.T) {
	cfg := OIDCConfig{
		IssuerURL: "https://issuer.example.com",
		ClientID:  "client-id",
		JWKSURL:   "https://jwks.example.com",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})

	handler := OIDCAuth(cfg)(inner)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "invalid_authorization" {
		t.Errorf("error = %q, want %q", body["error"], "invalid_authorization")
	}
}

func TestOIDCAuth_InvalidBearerFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"plain token without Bearer prefix", "some-token-value"},
		{"Bearer missing", "Bearer"},
		{"lowercase bearer", "bearer some-token"},
		{"extra spaces", "Bearer  extra  token"},
		{"token prefix only", "Bearer"},
	}

	cfg := OIDCConfig{
		IssuerURL: "https://issuer.example.com",
		ClientID:  "client-id",
		JWKSURL:   "https://jwks.example.com",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("next handler should not be called")
			})

			handler := OIDCAuth(cfg)(inner)

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", tt.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestOIDCAuth_ConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  OIDCConfig
	}{
		{
			name: "missing issuer",
			cfg:  OIDCConfig{ClientID: "c", JWKSURL: "https://jwks.example.com"},
		},
		{
			name: "missing client ID",
			cfg:  OIDCConfig{IssuerURL: "https://issuer.example.com", JWKSURL: "https://jwks.example.com"},
		},
		{
			name: "missing JWKS URL",
			cfg:  OIDCConfig{IssuerURL: "https://issuer.example.com", ClientID: "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("next handler should not be called")
			})

			handler := OIDCAuth(tt.cfg)(inner)

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", "Bearer some-token")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}

			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode response body: %v", err)
			}
			if body["error"] != "server_misconfigured" {
				t.Errorf("error = %q, want %q", body["error"], "server_misconfigured")
			}
		})
	}
}

func TestOIDCAuth_PassesProviderIDToContext(t *testing.T) {
	var capturedProviderID string
	var contextCalled bool

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextCalled = true
		pid, ok := GetProviderID(r.Context())
		if ok {
			capturedProviderID = pid
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.WithValue(context.Background(), ProviderIDKey, ProviderID("test-provider-42"))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	inner.ServeHTTP(rec, req)

	if !contextCalled {
		t.Fatal("inner handler was not called")
	}
	if capturedProviderID != "test-provider-42" {
		t.Errorf("captured provider_id = %q, want %q", capturedProviderID, "test-provider-42")
	}
}

func TestOIDCAuth_MissingAuthHeaderBodyFormat(t *testing.T) {
	cfg := OIDCConfig{
		IssuerURL: "https://issuer.example.com",
		ClientID:  "client-id",
		JWKSURL:   "https://jwks.example.com",
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := OIDCAuth(cfg)(inner)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), "application/json")
	}
}
