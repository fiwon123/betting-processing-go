package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type correlationKey string

const CorrelationIDKey correlationKey = "correlation_id"

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = r.Header.Get("X-Request-ID")
		}
		if correlationID == "" {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err == nil {
				correlationID = hex.EncodeToString(b)
			}
		}

		ctx := context.WithValue(r.Context(), CorrelationIDKey, correlationID)
		w.Header().Set("X-Correlation-ID", correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return id
	}
	return ""
}
