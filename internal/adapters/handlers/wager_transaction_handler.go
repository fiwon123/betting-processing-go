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

type WalletLookup interface {
	GetWallet(ctx context.Context, id string) (*wallet.Wallet, error)
}

type WagerTransactionHandler struct {
	service    WagerTransactionService
	walletLook WalletLookup
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

func NewWagerTransactionHandler(service WagerTransactionService, walletLook WalletLookup) *WagerTransactionHandler {
	return &WagerTransactionHandler{service: service, walletLook: walletLook}
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

// Create godoc
// @Summary Process a wager transaction
// @Description Processes BET, WIN, LOSS, REFUND, or ROLLBACK transactions. Requires Idempotency-Key header.
// @Tags wagering
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "Idempotency key (provider:externalId)"
// @Param request body WagerTransactionRequest true "Wager transaction request"
// @Success 201 {object} WagerTransactionResponse
// @Success 200 {object} WagerTransactionResponse
// @Success 202 {object} WagerTransactionResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security BearerAuth
// @Router /wagering/transactions [post]
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

		wallet, err := h.walletLook.GetWallet(r.Context(), req.WalletID)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error":   "wallet_not_found",
				"message": "wallet not found",
			})
			return
		}
		if wallet.ProviderID() != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error":   "provider_mismatch",
				"message": "wallet belongs to another provider",
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

// Get godoc
// @Summary Get a wager transaction by ID
// @Description Retrieves a wager transaction by internal ID. Provider isolation enforced.
// @Tags wagering
// @Produce json
// @Param transactionId path string true "Transaction ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /wagering/transactions/{transactionId} [get]
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

// GetByProviderAndExternalID godoc
// @Summary Get a wager transaction by provider and external ID
// @Description Retrieves a wager transaction by provider ID and external transaction ID
// @Tags wagering
// @Produce json
// @Param providerId path string true "Provider ID"
// @Param externalTransactionId path string true "External transaction ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /providers/{providerId}/wagering/transactions/{externalTransactionId} [get]
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
