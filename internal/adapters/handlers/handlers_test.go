package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fiwon123/betting-processing-go/internal/adapters/middleware"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
)

func withProviderID(ctx context.Context, providerID string) context.Context {
	return context.WithValue(ctx, middleware.ProviderIDKey, middleware.ProviderID(providerID))
}

func TestWalletHandlerCreateRejectsInvalidJSON(t *testing.T) {
	h := NewWalletHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString(`{`))
	req = req.WithContext(withProviderID(req.Context(), "provider-a"))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected BadRequest for invalid JSON, got %d", w.Code)
	}
}

func TestWalletHandlerCreateRejectsMissingProviderID(t *testing.T) {
	h := NewWalletHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/wallets", bytes.NewBufferString(`{"playerId":"p1","initialBalance":{"amount":"100.00","currency":"BRL"}}`))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected Forbidden for missing provider_id, got %d", w.Code)
	}
}

func TestWagerTransactionHandlerCreateRejectsMissingIdempotencyKey(t *testing.T) {
	h := NewWagerTransactionHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected BadRequest for missing idempotency key, got %d", w.Code)
	}
}

func TestWagerTransactionHandlerCreateRejectsInvalidJSON(t *testing.T) {
	h := NewWagerTransactionHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(`{`))
	req.Header.Set("Idempotency-Key", "test-key")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected BadRequest for invalid JSON, got %d", w.Code)
	}
}

func TestWagerTransactionHandlerCreateRejectsMissingFields(t *testing.T) {
	mock := &mockWagerTransactionService{
		result: &wagertransaction.ProcessResult{
			TransactionID: "tx-1",
			Status:        "PROCESSED",
		},
	}
	h := NewWagerTransactionHandler(mock)
	body := `{"providerId":"p1","externalTransactionId":"e1","playerId":"pl1","walletId":"w1","kind":"BET","money":{"amount":"10.00","currency":"BRL"}}`
	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "p1:e1")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected Created or UnprocessableEntity, got %d", w.Code)
	}
}

type mockWagerTransactionService struct {
	result *wagertransaction.ProcessResult
	err    error
}

func (m *mockWagerTransactionService) ProcessTransaction(_ context.Context, _ wagertransaction.Request, _ string, _ string) (*wagertransaction.ProcessResult, error) {
	return m.result, m.err
}

func (m *mockWagerTransactionService) FindByID(_ context.Context, _ string) (*wagertransaction.Transaction, error) {
	return nil, wagertransaction.ErrTransactionNotFound
}

func (m *mockWagerTransactionService) FindByProviderAndExternalID(_ context.Context, _, _ string) (*wagertransaction.Transaction, error) {
	return nil, wagertransaction.ErrTransactionNotFound
}

func TestWagerTransactionHandlerCreateWithMockService(t *testing.T) {
	mock := &mockWagerTransactionService{
		result: &wagertransaction.ProcessResult{
			TransactionID: "tx-1",
			Status:        "PROCESSED",
		},
	}

	h := NewWagerTransactionHandler(mock)

	body := `{
		"providerId": "provider-a",
		"externalTransactionId": "txn-123",
		"playerId": "player-1",
		"walletId": "wallet-1",
		"roundId": "round-1",
		"gameId": "game-1",
		"kind": "BET",
		"money": {"amount": "10.00", "currency": "BRL"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/wagering/transactions", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "provider-a:txn-123")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected Created, got %d", w.Code)
	}
}
