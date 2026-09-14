package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fiwon123/betting-processing-go/internal/adapters/middleware"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"github.com/go-chi/chi/v5"
)

type WagerTransactionService interface {
	ProcessTransaction(ctx context.Context, req wagertransaction.Request, idempotencyKey string, messageID string) (*wagertransaction.ProcessResult, error)
	FindByID(ctx context.Context, id string) (*wagertransaction.Transaction, error)
	FindByProviderAndExternalID(ctx context.Context, providerID, externalID string) (*wagertransaction.Transaction, error)
}

type WagerTransactionHandler struct {
	service WagerTransactionService
}

type WagerTransactionRequest struct {
	ProviderID            string   `json:"providerId"`
	ExternalTransactionID string   `json:"externalTransactionId"`
	PlayerID              string   `json:"playerId"`
	WalletID              string   `json:"walletId"`
	RoundID               string   `json:"roundId"`
	GameID                string   `json:"gameId"`
	Kind                  string   `json:"kind"`
	Money                 MoneyDTO `json:"money"`
	ReferenceExternalID   string   `json:"referenceExternalTransactionId,omitempty"`
}

type WagerTransactionResponse struct {
	TransactionID    string   `json:"transactionId"`
	Status           string   `json:"status"`
	Balance          MoneyDTO `json:"balance"`
	IdempotentReplay bool     `json:"idempotentReplay"`
}

func NewWagerTransactionHandler(service WagerTransactionService) *WagerTransactionHandler {
	return &WagerTransactionHandler{service: service}
}

func (h *WagerTransactionHandler) RegisterRoutes(r chi.Router) {
	r.Route("/wagering", func(r chi.Router) {
		r.Post("/transactions", h.Create)
		r.Get("/transactions/{transactionId}", h.Get)
	})
	r.Route("/providers/{providerId}", func(r chi.Router) {
		r.Get("/wagering/transactions/{externalTransactionId}", h.GetByProviderAndExternalID)
	})
}

func (h *WagerTransactionHandler) Create(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(idempotencyKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_idempotency_key",
			"message": "Idempotency-Key header is required",
		})
		return
	}

	var req WagerTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_json",
			"message": "body must be a valid JSON object",
		})
		return
	}

	if strings.TrimSpace(req.ProviderID) == "" ||
		strings.TrimSpace(req.ExternalTransactionID) == "" ||
		strings.TrimSpace(req.PlayerID) == "" ||
		strings.TrimSpace(req.WalletID) == "" ||
		strings.TrimSpace(req.Kind) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "providerId, externalTransactionId, playerId, walletId, and kind are required",
		})
		return
	}

	if providerID, ok := middleware.GetProviderID(r.Context()); ok {
		if req.ProviderID != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "provider_mismatch",
				"message": "providerId in body does not match authenticated provider",
			})
			return
		}
	}

	if req.Kind != "LOSS" && (strings.TrimSpace(req.Money.Amount) == "" || strings.TrimSpace(req.Money.Currency) == "") {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_money",
			"message": "money.amount and money.currency are required for non-LOSS transactions",
		})
		return
	}

	if req.Money.Currency == "" {
		req.Money.Currency = "BRL"
	}

	if req.Kind == "LOSS" {
		req.Money.Amount = "0.00"
		if req.Money.Currency == "" {
			req.Money.Currency = "BRL"
		}
	}

	svcReq := wagertransaction.Request{
		ProviderID:            req.ProviderID,
		ExternalTransactionID: req.ExternalTransactionID,
		PlayerID:              req.PlayerID,
		WalletID:              req.WalletID,
		RoundID:               req.RoundID,
		GameID:                req.GameID,
		Kind:                  req.Kind,
		Money: wagertransaction.MoneyDTO{
			Amount:   req.Money.Amount,
			Currency: req.Money.Currency,
		},
		ReferenceExternalID: req.ReferenceExternalID,
	}

	result, err := h.service.ProcessTransaction(r.Context(), svcReq, idempotencyKey, "")
	if err != nil {
		switch {
		case errors.Is(err, wagertransaction.ErrPayloadConflict):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "payload_conflict",
				"message": "idempotency key reused with different payload",
			})
		case errors.Is(err, wagertransaction.ErrOPENINGRejected):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid_kind",
				"message": "OPENING is reserved for internal wallet creation",
			})
		case errors.Is(err, wagertransaction.ErrInvalidMoney):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid_money",
				"message": err.Error(),
			})
		case errors.Is(err, wagertransaction.ErrInvalidTransactionType):
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid_kind",
				"message": "kind must be BET, WIN, LOSS, REFUND, or ROLLBACK",
			})
		case errors.Is(err, wallet.ErrInsufficientBalance):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error":   "insufficient_balance",
				"message": "wallet does not have sufficient balance",
			})
		case errors.Is(err, wallet.ErrConcurrentUpdate):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error":   "concurrent_update",
				"message": "optimistic lock conflict, retry",
			})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error":   "internal_error",
				"message": "processing failed",
			})
		}
		return
	}

	status := http.StatusCreated
	if result.Status == "REJECTED" {
		status = http.StatusUnprocessableEntity
	}
	if result.IdempotentReplay {
		status = http.StatusOK
	}
	if result.Status == "PENDING" || result.Status == "PENDING_REFERENCE" {
		status = http.StatusAccepted
	}

	writeJSON(w, status, WagerTransactionResponse{
		TransactionID: result.TransactionID,
		Status:        result.Status,
		Balance: MoneyDTO{
			Amount:   result.Balance.String(),
			Currency: string(result.Balance.Currency()),
		},
		IdempotentReplay: result.IdempotentReplay,
	})
}

func (h *WagerTransactionHandler) Get(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "transactionId")
	if strings.TrimSpace(txID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_transaction_id"})
		return
	}

	tx, err := h.service.FindByID(r.Context(), txID)
	if err != nil {
		if errors.Is(err, wagertransaction.ErrTransactionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	if providerID, ok := middleware.GetProviderID(r.Context()); ok {
		if tx.Provider() != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "provider_mismatch", "message": "transaction belongs to another provider"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    tx.ID(),
		"origin":                string(tx.Origin()),
		"externalTransactionId": tx.ExternalID(),
		"providerId":            tx.Provider(),
		"walletId":              tx.WalletID(),
		"playerId":              tx.PlayerID(),
		"roundId":               tx.RoundID(),
		"gameId":                tx.GameID(),
		"kind":                  string(tx.TransactionType()),
		"amount": MoneyDTO{
			Amount:   tx.Amount().String(),
			Currency: string(tx.Amount().Currency()),
		},
		"externalReferenceId": tx.ExternalReference(),
		"internalReferenceId": tx.InternalReference(),
		"status":              string(tx.Status()),
		"failureCode":         tx.FailureCode(),
	})
}

func (h *WagerTransactionHandler) GetByProviderAndExternalID(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	externalID := chi.URLParam(r, "externalTransactionId")

	if strings.TrimSpace(providerID) == "" || strings.TrimSpace(externalID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_parameters"})
		return
	}

	if jwtProviderID, ok := middleware.GetProviderID(r.Context()); ok {
		if providerID != jwtProviderID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "provider_mismatch", "message": "providerId in URL does not match authenticated provider"})
			return
		}
	}

	tx, err := h.service.FindByProviderAndExternalID(r.Context(), providerID, externalID)
	if err != nil {
		if errors.Is(err, wagertransaction.ErrTransactionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "transaction_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                    tx.ID(),
		"origin":                string(tx.Origin()),
		"externalTransactionId": tx.ExternalID(),
		"providerId":            tx.Provider(),
		"walletId":              tx.WalletID(),
		"playerId":              tx.PlayerID(),
		"roundId":               tx.RoundID(),
		"gameId":                tx.GameID(),
		"kind":                  string(tx.TransactionType()),
		"amount": MoneyDTO{
			Amount:   tx.Amount().String(),
			Currency: string(tx.Amount().Currency()),
		},
		"externalReferenceId": tx.ExternalReference(),
		"internalReferenceId": tx.InternalReference(),
		"status":              string(tx.Status()),
		"failureCode":         tx.FailureCode(),
	})
}
