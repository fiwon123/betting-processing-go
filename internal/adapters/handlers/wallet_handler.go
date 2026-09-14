package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fiwon123/betting-processing-go/internal/adapters/middleware"
	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
	"github.com/go-chi/chi/v5"
)

type WalletService interface {
	CreateWallet(ctx context.Context, providerID, playerID string, initialBalance money.Money) (*wallet.Wallet, error)
	GetWallet(ctx context.Context, id string) (*wallet.Wallet, error)
	GetLedger(ctx context.Context, walletID, cursor string, limit int) ([]*wallet.LedgerEntry, string, error)
	Reconcile(ctx context.Context, walletID string) (*wallet.ReconciliationResult, error)
}

type WalletHandler struct {
	service WalletService
}

type WalletCreateRequest struct {
	PlayerID       string `json:"playerId"`
	InitialBalance struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"initialBalance"`
}

type MoneyDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func NewWalletHandler(service WalletService) *WalletHandler {
	return &WalletHandler{service: service}
}

func (h *WalletHandler) RegisterRoutes(r chi.Router) {
	r.Route("/wallets", func(r chi.Router) {
		r.Post("/", h.Create)
		r.Get("/{walletId}", h.Get)
		r.Get("/{walletId}/ledger", h.GetLedger)
		r.Post("/{walletId}/reconciliation", h.Reconcile)
	})
}

func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	providerID, ok := middleware.GetProviderID(r.Context())
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":   "missing_provider_id",
			"message": "authenticated identity must have a provider_id",
		})
		return
	}

	var req WalletCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_json",
			"message": "body must be a valid JSON object",
		})
		return
	}

	if strings.TrimSpace(req.PlayerID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_player_id",
			"message": "playerId is required",
		})
		return
	}

	if req.InitialBalance.Currency == "" {
		req.InitialBalance.Currency = "BRL"
	}

	parsed, err := money.ParseMoney(req.InitialBalance.Amount, money.Currency(strings.ToUpper(req.InitialBalance.Currency)))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_money",
			"message": "initialBalance.amount must be a valid decimal with at most 2 decimal places",
		})
		return
	}

	created, err := h.service.CreateWallet(r.Context(), providerID, req.PlayerID, parsed)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrDuplicateWallet):
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":   "duplicate_wallet",
				"message": "wallet already exists for this player, currency and provider",
			})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "create_wallet_failed",
				"message": err.Error(),
			})
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         created.ID(),
		"playerId":   created.PlayerID(),
		"providerId": created.ProviderID(),
		"balance": MoneyDTO{
			Amount:   created.Balance().String(),
			Currency: string(created.Currency()),
		},
		"version": created.Version(),
	})
}

func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	if strings.TrimSpace(walletID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_wallet_id"})
		return
	}

	wal, err := h.service.GetWallet(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	if providerID, ok := middleware.GetProviderID(r.Context()); ok {
		if wal.ProviderID() != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "provider_mismatch", "message": "wallet belongs to another provider"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         wal.ID(),
		"playerId":   wal.PlayerID(),
		"providerId": wal.ProviderID(),
		"balance": MoneyDTO{
			Amount:   wal.Balance().String(),
			Currency: string(wal.Currency()),
		},
		"version": wal.Version(),
	})
}

func (h *WalletHandler) GetLedger(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	if strings.TrimSpace(walletID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_wallet_id"})
		return
	}

	if providerID, ok := middleware.GetProviderID(r.Context()); ok {
		wal, err := h.service.GetWallet(r.Context(), walletID)
		if err != nil {
			if errors.Is(err, wallet.ErrWalletNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet_not_found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if wal.ProviderID() != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "provider_mismatch", "message": "wallet belongs to another provider"})
			return
		}
	}

	limitRaw := r.URL.Query().Get("limit")
	limit := 50
	if limitRaw != "" {
		v, err := strconv.Atoi(limitRaw)
		if err == nil && v > 0 {
			limit = v
		}
	}

	cursor := r.URL.Query().Get("cursor")
	entries, nextCursor, err := h.service.GetLedger(r.Context(), walletID, cursor, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	type EntryDTO struct {
		ID            string   `json:"id"`
		WalletID      string   `json:"walletId"`
		TransactionID string   `json:"transactionId"`
		Direction     string   `json:"direction"`
		Amount        MoneyDTO `json:"amount"`
		BalanceBefore MoneyDTO `json:"balanceBefore"`
		BalanceAfter  MoneyDTO `json:"balanceAfter"`
	}

	var entryDTOs []EntryDTO
	for _, e := range entries {
		entryDTOs = append(entryDTOs, EntryDTO{
			ID:            e.ID(),
			WalletID:      e.WalletID(),
			TransactionID: e.TransactionID(),
			Direction:     string(e.Direction()),
			Amount: MoneyDTO{
				Amount:   e.Amount().String(),
				Currency: string(e.Currency()),
			},
			BalanceBefore: MoneyDTO{
				Amount:   e.BalanceBefore().String(),
				Currency: string(e.Currency()),
			},
			BalanceAfter: MoneyDTO{
				Amount:   e.BalanceAfter().String(),
				Currency: string(e.Currency()),
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"walletId":   walletID,
		"entries":    entryDTOs,
		"nextCursor": nextCursor,
		"hasMore":    nextCursor != "",
	})
}

func (h *WalletHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")
	if strings.TrimSpace(walletID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_wallet_id"})
		return
	}

	if providerID, ok := middleware.GetProviderID(r.Context()); ok {
		wal, err := h.service.GetWallet(r.Context(), walletID)
		if err != nil {
			if errors.Is(err, wallet.ErrWalletNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet_not_found"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		if wal.ProviderID() != providerID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "provider_mismatch", "message": "wallet belongs to another provider"})
			return
		}
	}

	result, err := h.service.Reconcile(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "wallet_not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"walletId":          result.WalletID,
		"storedBalance":     MoneyDTO{Amount: result.StoredBalance.String(), Currency: string(result.StoredBalance.Currency())},
		"calculatedBalance": MoneyDTO{Amount: result.CalculatedBalance.String(), Currency: string(result.CalculatedBalance.Currency())},
		"difference":        MoneyDTO{Amount: result.Difference.String(), Currency: string(result.Difference.Currency())},
		"consistent":        result.Consistent,
		"checkedEntries":    result.CheckedEntries,
	})
}
