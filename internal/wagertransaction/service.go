package wagertransaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/fiwon123/betting-processing-go/internal/infra/metrics"
	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wallet"
)

const (
	consumerName = "sqs-wager-transactions"
)

type WalletService interface {
	Debit(ctx context.Context, dbTx domain.DBTx, walletID string, amount money.Money) (balanceBefore money.Money, balanceAfter money.Money, walletVersion int64, err error)
	Credit(ctx context.Context, dbTx domain.DBTx, walletID string, amount money.Money) (balanceBefore money.Money, balanceAfter money.Money, walletVersion int64, err error)
	GetBalance(ctx context.Context, walletID string) (money.Money, error)
}

type Service struct {
	repo       Repository
	inboxRepo  InboxRepository
	outboxRepo OutboxRepository
	walletSvc  WalletService
	txFactory  domain.DBTxFactory
	metrics    *metrics.Metrics
}

func NewService(
	repo Repository,
	inboxRepo InboxRepository,
	outboxRepo OutboxRepository,
	walletSvc WalletService,
	txFactory domain.DBTxFactory,
	m *metrics.Metrics,
) *Service {
	return &Service{
		repo:       repo,
		inboxRepo:  inboxRepo,
		outboxRepo: outboxRepo,
		walletSvc:  walletSvc,
		txFactory:  txFactory,
		metrics:    m,
	}
}

type ProcessResult struct {
	TransactionID    string
	Status           string
	Balance          money.Money
	IdempotentReplay bool
}

type TransactionDTO struct {
	ID                string   `json:"id"`
	Origin            string   `json:"origin"`
	ExternalID        string   `json:"externalTransactionId"`
	ProviderID        string   `json:"providerId"`
	WalletID          string   `json:"walletId"`
	PlayerID          string   `json:"playerId"`
	RoundID           string   `json:"roundId"`
	GameID            string   `json:"gameId"`
	Kind              string   `json:"kind"`
	Amount            MoneyDTO `json:"amount"`
	ExternalReference string   `json:"externalReferenceId,omitempty"`
	InternalReference string   `json:"internalReferenceId,omitempty"`
	Status            string   `json:"status"`
	FailureCode       string   `json:"failureCode,omitempty"`
}

func (s *Service) FindByID(ctx context.Context, id string) (*Transaction, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) FindByProviderAndExternalID(ctx context.Context, providerID, externalID string) (*Transaction, error) {
	return s.repo.FindByProviderAndExternalID(ctx, providerID, externalID)
}

func (s *Service) ProcessTransaction(ctx context.Context, req Request, idempotencyKey string, messageID string) (*ProcessResult, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.ProcessingDuration.Observe(time.Since(start).Seconds())
		}
	}()

	if idempotencyKey == "" {
		idempotencyKey = req.IdempotencyKey
	}
	if idempotencyKey == "" {
		idempotencyKey = req.ProviderID + ":" + req.ExternalTransactionID
	}

	existing, err := s.repo.FindByIdempotencyKey(ctx, req.ProviderID, idempotencyKey)
	if err == nil && existing != nil {
		if s.metrics != nil {
			s.metrics.DuplicatesTotal.Inc()
		}
		payloadHash := domain.ComputePayloadHash(
			req.ProviderID, req.ExternalTransactionID, req.PlayerID,
			req.WalletID, req.RoundID, req.GameID, req.Kind,
			req.Money.Amount, req.Money.Currency, req.ReferenceExternalID,
		)
		if existing.PayloadHash() != "" && existing.PayloadHash() != payloadHash {
			return nil, ErrPayloadConflict
		}
		return s.handleReplay(ctx, existing)
	}
	if err != nil && err != ErrTransactionNotFound {
		return nil, fmt.Errorf("check idempotency: %w", err)
	}

	extExists, err := s.repo.ExternalTransactionExists(ctx, req.ProviderID, req.ExternalTransactionID, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("check external id: %w", err)
	}
	if extExists {
		return nil, ErrExternalIDReuse
	}

	kind := TransactionType(req.Kind)
	if !kind.Valid() {
		return nil, ErrInvalidTransactionType
	}

	parsed, err := money.ParseMoney(req.Money.Amount, money.Currency(req.Money.Currency))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMoney, err)
	}

	payloadHash := domain.ComputePayloadHash(
		req.ProviderID, req.ExternalTransactionID, req.PlayerID,
		req.WalletID, req.RoundID, req.GameID, req.Kind,
		req.Money.Amount, req.Money.Currency, req.ReferenceExternalID,
	)

	tx, err := NewExternalTransaction(
		"", req.ProviderID, req.ExternalTransactionID,
		idempotencyKey, payloadHash,
		req.WalletID, req.PlayerID, req.RoundID, req.GameID,
		kind, parsed, req.ReferenceExternalID,
	)
	if err != nil {
		return nil, err
	}

	var result *ProcessResult
	switch kind {
	case BET:
		result, err = s.processBet(ctx, tx, parsed, messageID)
	case WIN:
		result, err = s.processWin(ctx, tx, req, parsed, messageID)
	case LOSS:
		result, err = s.processLoss(ctx, tx, messageID)
	case REFUND:
		result, err = s.processRefund(ctx, tx, req, parsed, messageID)
	case ROLLBACK:
		result, err = s.processRollback(ctx, tx, req, parsed, messageID)
	default:
		return nil, ErrInvalidTransactionType
	}

	if err != nil && errors.Is(err, ErrIdempotentDuplicate) {
		existing, findErr := s.repo.FindByIdempotencyKey(ctx, req.ProviderID, idempotencyKey)
		if findErr == nil && existing != nil {
			return s.handleReplay(ctx, existing)
		}
	}
	return result, err
}

func (s *Service) handleReplay(ctx context.Context, existing *Transaction) (*ProcessResult, error) {
	balance := existing.ResultBalance()
	if balance == nil {
		w, err := s.getWalletBalance(ctx, existing.WalletID())
		if err != nil {
			return nil, err
		}
		balance = &w
	}
	return &ProcessResult{
		TransactionID:    existing.ID(),
		Status:           string(existing.Status()),
		Balance:          *balance,
		IdempotentReplay: true,
	}, nil
}

func (s *Service) processBet(ctx context.Context, tx *Transaction, amount money.Money, messageID string) (*ProcessResult, error) {
	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin bet transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	balBefore, balAfter, walletVersion, err := s.walletSvc.Debit(ctx, dbTx, tx.WalletID(), amount)
	if err != nil {
		_ = dbTx.Rollback(ctx)
		failureCode := mapWalletError(err)
		tx.SetFailureCode(failureCode)
		if failureCode == "INSUFFICIENT_BALANCE" || failureCode == "CURRENCY_MISMATCH" {
			w, wErr := s.getWalletBalance(ctx, tx.WalletID())
			if wErr == nil {
				tx.SetResultBalance(w)
			}
		}
		_ = s.createAndReject(ctx, tx, failureCode)
		if errors.Is(err, wallet.ErrConcurrentUpdate) {
			if s.metrics != nil {
				s.metrics.ConcurrencyConflicts.Inc()
			}
			return nil, err
		}
		if s.metrics != nil {
			s.metrics.TransactionsTotal.WithLabelValues("REJECTED").Inc()
		}
		return nil, fmt.Errorf("%w: %w", ErrInsufficientBalance, err)
	}

	tx.SetResultBalance(balAfter)
	err = s.commitWithLedger(ctx, dbTx, tx, domain.DEBIT, amount, balBefore, balAfter, walletVersion, messageID)
	if err != nil {
		return nil, err
	}

	if s.metrics != nil {
		s.metrics.TransactionsTotal.WithLabelValues("PROCESSED").Inc()
	}
	return s.result(tx, balAfter)
}

func (s *Service) processWin(ctx context.Context, tx *Transaction, req Request, amount money.Money, messageID string) (*ProcessResult, error) {
	if req.ReferenceExternalID != "" {
		ref, err := s.repo.FindByProviderAndExternalID(ctx, req.ProviderID, req.ReferenceExternalID)
		if err != nil {
			if errors.Is(err, ErrTransactionNotFound) {
				return s.reject(ctx, tx, "REFERENCE_NOT_FOUND")
			}
			return nil, err
		}
		if ref.TransactionType() != BET {
			return s.reject(ctx, tx, "REFERENCE_NOT_FOUND")
		}
		if ref.PlayerID() != tx.PlayerID() {
			return s.reject(ctx, tx, "PLAYER_MISMATCH")
		}
		if ref.WalletID() != tx.WalletID() {
			return s.reject(ctx, tx, "WALLET_MISMATCH")
		}
		if ref.Amount().Currency() != tx.Amount().Currency() {
			return s.reject(ctx, tx, "CURRENCY_MISMATCH")
		}
		if ref.RoundID() != tx.RoundID() {
			return s.reject(ctx, tx, "ROUND_MISMATCH")
		}
	}

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin win transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	balBefore, balAfter, walletVersion, err := s.walletSvc.Credit(ctx, dbTx, tx.WalletID(), amount)
	if err != nil {
		_ = dbTx.Rollback(ctx)
		failureCode := mapWalletError(err)
		tx.SetFailureCode(failureCode)
		if failureCode == "INSUFFICIENT_BALANCE" || failureCode == "CURRENCY_MISMATCH" {
			w, wErr := s.getWalletBalance(ctx, tx.WalletID())
			if wErr == nil {
				tx.SetResultBalance(w)
			}
		}
		_ = s.createAndReject(ctx, tx, failureCode)
		if errors.Is(err, wallet.ErrConcurrentUpdate) {
			if s.metrics != nil {
				s.metrics.ConcurrencyConflicts.Inc()
			}
			return nil, err
		}
		if s.metrics != nil {
			s.metrics.TransactionsTotal.WithLabelValues("REJECTED").Inc()
		}
		return nil, fmt.Errorf("%w: %w", ErrInsufficientBalance, err)
	}

	tx.SetResultBalance(balAfter)
	err = s.commitWithLedger(ctx, dbTx, tx, domain.CREDIT, amount, balBefore, balAfter, walletVersion, messageID)
	if err != nil {
		return nil, err
	}

	if s.metrics != nil {
		s.metrics.TransactionsTotal.WithLabelValues("PROCESSED").Inc()
	}
	return s.result(tx, balAfter)
}

func (s *Service) processLoss(ctx context.Context, tx *Transaction, messageID string) (*ProcessResult, error) {
	tx.TransitionTo(PROCESSED)

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin loss transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	w, err := s.getWalletBalance(ctx, tx.WalletID())
	if err != nil {
		return nil, err
	}
	tx.SetResultBalance(w)

	if messageID != "" {
		if _, err := s.inboxRepo.RecordReceivedTx(ctx, dbTx, consumerName, messageID, ""); err != nil {
			return nil, fmt.Errorf("record inbox: %w", err)
		}
	}

	txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
	if err != nil {
		return nil, fmt.Errorf("insert loss transaction: %w", err)
	}
	tx.SetID(txID)

	processedEvent := domain.Event{
		EventID:       fmt.Sprintf("evt-%s-processed", txID),
		CorrelationID: txID,
		EventType:     "WagerTransactionProcessed",
		AggregateType: "WagerTransaction",
		AggregateID:   txID,
		OccurredAt:    tx.UpdatedAt(),
		Version:       1,
		Data: domain.WagerTransactionProcessedData{
			TransactionID: txID,
			ExternalID:    tx.ExternalID(),
			ProviderID:    tx.Provider(),
			PlayerID:      tx.PlayerID(),
			WalletID:      tx.WalletID(),
			Kind:          string(tx.TransactionType()),
			Money:         tx.Amount(),
		},
	}
	payload, _ := json.Marshal(processedEvent)
	err = s.repo.CreateOutboxEventTx(ctx, dbTx, processedEvent.AggregateType, processedEvent.AggregateID, processedEvent.EventType, payload)
	if err != nil {
		return nil, fmt.Errorf("insert processed outbox event: %w", err)
	}

	if messageID != "" {
		_ = s.inboxRepo.MarkCompletedTx(ctx, dbTx, consumerName, messageID)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit loss: %w", err)
	}

	if s.metrics != nil {
		s.metrics.TransactionsTotal.WithLabelValues("PROCESSED").Inc()
	}

	w2, err := s.getWalletBalance(ctx, tx.WalletID())
	if err != nil {
		return nil, err
	}

	return s.result(tx, w2)
}

func (s *Service) processRefund(ctx context.Context, tx *Transaction, req Request, amount money.Money, messageID string) (*ProcessResult, error) {
	if req.ReferenceExternalID == "" {
		return s.reject(ctx, tx, "MISSING_REFERENCE")
	}

	ref, err := s.repo.FindByProviderAndExternalID(ctx, req.ProviderID, req.ReferenceExternalID)
	if err != nil {
		if errors.Is(err, ErrTransactionNotFound) {
			return s.reject(ctx, tx, "REFERENCE_NOT_FOUND")
		}
		return nil, err
	}

	if err := s.validateReference(tx, ref); err != nil {
		return s.reject(ctx, tx, mapValidationError(err))
	}

	if ref.Status() != PROCESSED {
		if ref.Status() == PENDING || ref.Status() == PENDING_REFERENCE {
			tx.TransitionTo(PENDING_REFERENCE)

			dbTx, err := s.txFactory.Begin(ctx)
			if err != nil {
				return nil, fmt.Errorf("begin pending-ref transaction: %w", err)
			}
			defer dbTx.Rollback(ctx)

			txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
			if err != nil {
				return nil, fmt.Errorf("insert pending-ref transaction: %w", err)
			}
			tx.SetID(txID)

			pendingEvent := domain.Event{
				EventID:       fmt.Sprintf("evt-%s-pending-ref", txID),
				CorrelationID: txID,
				EventType:     "WagerTransactionPendingReference",
				AggregateType: "WagerTransaction",
				AggregateID:   txID,
				OccurredAt:    tx.UpdatedAt(),
				Version:       1,
				Data: domain.WagerTransactionPendingReferenceData{
					TransactionID:  txID,
					ExternalID:     tx.ExternalID(),
					ProviderID:     tx.Provider(),
					ReferenceExtID: tx.ExternalReference(),
					PlayerID:       tx.PlayerID(),
					WalletID:       tx.WalletID(),
					Kind:           string(tx.TransactionType()),
				},
			}
			payload, _ := json.Marshal(pendingEvent)
			err = s.repo.CreateOutboxEventTx(ctx, dbTx, pendingEvent.AggregateType, pendingEvent.AggregateID, pendingEvent.EventType, payload)
			if err != nil {
				return nil, fmt.Errorf("insert pending-ref outbox event: %w", err)
			}

			if err := dbTx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit pending-ref: %w", err)
			}

			return &ProcessResult{
				TransactionID: tx.ID(),
				Status:        string(PENDING_REFERENCE),
			}, nil
		}
		return s.reject(ctx, tx, "REFERENCE_NOT_SUCCESSFUL")
	}

	hasReversal, _ := s.repo.HasSuccessfulReversal(ctx, ref.ID())
	if hasReversal {
		return s.reject(ctx, tx, "DOUBLE_REVERSAL")
	}

	if err := s.validateReversalValue(amount, ref); err != nil {
		return s.reject(ctx, tx, "REVERSAL_VALUE_MISMATCH")
	}

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	balBefore, balAfter, walletVersion, err := s.walletSvc.Credit(ctx, dbTx, tx.WalletID(), amount)
	if err != nil {
		_ = dbTx.Rollback(ctx)
		failureCode := mapWalletErrorForReversal(err)
		tx.SetFailureCode(failureCode)
		if failureCode == "REVERSAL_INSUFFICIENT_BALANCE" || failureCode == "CURRENCY_MISMATCH" {
			w, wErr := s.getWalletBalance(ctx, tx.WalletID())
			if wErr == nil {
				tx.SetResultBalance(w)
			}
		}
		_ = s.createAndReject(ctx, tx, failureCode)
		return nil, err
	}

	tx.SetInternalReference(ref.ID())
	tx.SetResultBalance(balAfter)
	err = s.commitWithLedger(ctx, dbTx, tx, domain.CREDIT, amount, balBefore, balAfter, walletVersion, messageID)
	if err != nil {
		return nil, err
	}

	return s.result(tx, balAfter)
}

func (s *Service) processRollback(ctx context.Context, tx *Transaction, req Request, amount money.Money, messageID string) (*ProcessResult, error) {
	if req.ReferenceExternalID == "" {
		return s.reject(ctx, tx, "MISSING_REFERENCE")
	}

	ref, err := s.repo.FindByProviderAndExternalID(ctx, req.ProviderID, req.ReferenceExternalID)
	if err != nil {
		if errors.Is(err, ErrTransactionNotFound) {
			return s.reject(ctx, tx, "REFERENCE_NOT_FOUND")
		}
		return nil, err
	}

	if err := s.validateReference(tx, ref); err != nil {
		return s.reject(ctx, tx, mapValidationError(err))
	}

	if ref.Status() != PROCESSED {
		if ref.Status() == PENDING || ref.Status() == PENDING_REFERENCE {
			tx.TransitionTo(PENDING_REFERENCE)

			dbTx, err := s.txFactory.Begin(ctx)
			if err != nil {
				return nil, fmt.Errorf("begin pending-ref transaction: %w", err)
			}
			defer dbTx.Rollback(ctx)

			txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
			if err != nil {
				return nil, fmt.Errorf("insert pending-ref transaction: %w", err)
			}
			tx.SetID(txID)

			pendingEvent := domain.Event{
				EventID:       fmt.Sprintf("evt-%s-pending-ref", txID),
				CorrelationID: txID,
				EventType:     "WagerTransactionPendingReference",
				AggregateType: "WagerTransaction",
				AggregateID:   txID,
				OccurredAt:    tx.UpdatedAt(),
				Version:       1,
				Data: domain.WagerTransactionPendingReferenceData{
					TransactionID:  txID,
					ExternalID:     tx.ExternalID(),
					ProviderID:     tx.Provider(),
					ReferenceExtID: tx.ExternalReference(),
					PlayerID:       tx.PlayerID(),
					WalletID:       tx.WalletID(),
					Kind:           string(tx.TransactionType()),
				},
			}
			payload, _ := json.Marshal(pendingEvent)
			err = s.repo.CreateOutboxEventTx(ctx, dbTx, pendingEvent.AggregateType, pendingEvent.AggregateID, pendingEvent.EventType, payload)
			if err != nil {
				return nil, fmt.Errorf("insert pending-ref outbox event: %w", err)
			}

			if err := dbTx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit pending-ref: %w", err)
			}

			return &ProcessResult{
				TransactionID: tx.ID(),
				Status:        string(PENDING_REFERENCE),
			}, nil
		}
		return s.reject(ctx, tx, "REFERENCE_NOT_SUCCESSFUL")
	}

	hasReversal, _ := s.repo.HasSuccessfulReversal(ctx, ref.ID())
	if hasReversal {
		return s.reject(ctx, tx, "DOUBLE_REVERSAL")
	}

	if err := s.validateReversalValue(amount, ref); err != nil {
		return s.reject(ctx, tx, "REVERSAL_VALUE_MISMATCH")
	}

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin rollback transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	var dir domain.Direction
	var balBefore, balAfter money.Money
	var walletVersion int64

	switch ref.TransactionType() {
	case BET:
		balBefore, balAfter, walletVersion, err = s.walletSvc.Credit(ctx, dbTx, tx.WalletID(), amount)
		dir = domain.CREDIT
	case WIN:
		balBefore, balAfter, walletVersion, err = s.walletSvc.Debit(ctx, dbTx, tx.WalletID(), amount)
		dir = domain.DEBIT
	case REFUND:
		balBefore, balAfter, walletVersion, err = s.walletSvc.Debit(ctx, dbTx, tx.WalletID(), amount)
		dir = domain.DEBIT
	default:
		return s.reject(ctx, tx, "INVALID_REFERENCE_TYPE")
	}

	if err != nil {
		_ = dbTx.Rollback(ctx)
		failureCode := mapWalletErrorForReversal(err)
		tx.SetFailureCode(failureCode)
		if failureCode == "REVERSAL_INSUFFICIENT_BALANCE" || failureCode == "CURRENCY_MISMATCH" {
			w, wErr := s.getWalletBalance(ctx, tx.WalletID())
			if wErr == nil {
				tx.SetResultBalance(w)
			}
		}
		_ = s.createAndReject(ctx, tx, failureCode)
		return nil, err
	}

	tx.SetInternalReference(ref.ID())
	tx.SetResultBalance(balAfter)
	err = s.commitWithLedger(ctx, dbTx, tx, dir, amount, balBefore, balAfter, walletVersion, messageID)
	if err != nil {
		return nil, err
	}

	return s.result(tx, balAfter)
}

const maxRefAttempts = 10

func (s *Service) ResolvePendingReference(ctx context.Context, txID string) (*ProcessResult, error) {
	tx, err := s.repo.FindByID(ctx, txID)
	if err != nil {
		return nil, fmt.Errorf("find pending transaction: %w", err)
	}
	if tx.Status() != PENDING_REFERENCE {
		return s.result(tx, money.Money{})
	}

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	claimed, err := s.repo.ClaimPendingReferenceTx(ctx, dbTx, txID)
	if err != nil {
		return nil, fmt.Errorf("claim pending reference: %w", err)
	}
	if err := dbTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}

	if !claimed {
		tx, err = s.repo.FindByID(ctx, txID)
		if err != nil {
			return nil, err
		}
		return s.result(tx, money.Money{})
	}

	if tx.RefAttempts() >= maxRefAttempts {
		return s.resolveRejectInPlace(ctx, tx, "REFERENCE_NOT_FOUND")
	}

	ref, err := s.repo.FindByProviderAndExternalID(ctx, tx.Provider(), tx.ExternalReference())
	if err != nil {
		if errors.Is(err, ErrTransactionNotFound) {
			_ = s.repo.IncrementRefAttempts(ctx, tx.ID())
			return s.resolveRejectInPlace(ctx, tx, "REFERENCE_NOT_FOUND")
		}
		return nil, err
	}

	if ref.Status() == PENDING || ref.Status() == PENDING_REFERENCE {
		_ = s.repo.IncrementRefAttempts(ctx, tx.ID())
		if s.metrics != nil {
			s.metrics.RetriesTotal.Inc()
		}
		return &ProcessResult{
			TransactionID: tx.ID(),
			Status:        string(PENDING_REFERENCE),
		}, nil
	}

	if ref.Status() == REJECTED || ref.Status() == FAILED {
		return s.resolveRejectInPlace(ctx, tx, "REFERENCE_NOT_SUCCESSFUL")
	}

	if ref.Status() == PROCESSED {
		hasReversal, _ := s.repo.HasSuccessfulReversal(ctx, ref.ID())
		if hasReversal {
			return s.resolveRejectInPlace(ctx, tx, "DOUBLE_REVERSAL")
		}

		amount := tx.Amount()
		if err := s.validateReversalValue(amount, ref); err != nil {
			return s.resolveRejectInPlace(ctx, tx, "REVERSAL_VALUE_MISMATCH")
		}

		resTx, err := s.txFactory.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("begin resolve-ref transaction: %w", err)
		}
		defer resTx.Rollback(ctx)

		var dir domain.Direction
		var balBefore, balAfter money.Money
		var walletVersion int64

		switch tx.TransactionType() {
		case REFUND:
			balBefore, balAfter, walletVersion, err = s.walletSvc.Credit(ctx, resTx, tx.WalletID(), amount)
			dir = domain.CREDIT
		case ROLLBACK:
			switch ref.TransactionType() {
			case BET:
				balBefore, balAfter, walletVersion, err = s.walletSvc.Credit(ctx, resTx, tx.WalletID(), amount)
				dir = domain.CREDIT
			case WIN, REFUND:
				balBefore, balAfter, walletVersion, err = s.walletSvc.Debit(ctx, resTx, tx.WalletID(), amount)
				dir = domain.DEBIT
			default:
				return s.resolveRejectInPlace(ctx, tx, "INVALID_REFERENCE_TYPE")
			}
		default:
			return s.resolveRejectInPlace(ctx, tx, "INVALID_REFERENCE_TYPE")
		}

		if err != nil {
			_ = resTx.Rollback(ctx)
			failureCode := mapWalletErrorForReversal(err)
			tx.SetFailureCode(failureCode)

			err = s.repo.UpdateStatusTx(ctx, resTx, tx.ID(), REJECTED, failureCode)
			if err != nil {
				return nil, fmt.Errorf("update rejected status: %w", err)
			}

			rejectedEvent := domain.Event{
				EventID:       fmt.Sprintf("evt-%s-rejected", tx.ID()),
				CorrelationID: tx.ID(),
				EventType:     "WagerTransactionRejected",
				AggregateType: "WagerTransaction",
				AggregateID:   tx.ID(),
				OccurredAt:    tx.UpdatedAt(),
				Version:       1,
				Data: domain.WagerTransactionRejectedData{
					TransactionID: tx.ID(),
					ExternalID:    tx.ExternalID(),
					ProviderID:    tx.Provider(),
					PlayerID:      tx.PlayerID(),
					WalletID:      tx.WalletID(),
					Kind:          string(tx.TransactionType()),
					Money:         tx.Amount(),
					FailureCode:   failureCode,
				},
			}
			payload, _ := json.Marshal(rejectedEvent)
			err = s.repo.CreateOutboxEventTx(ctx, resTx, rejectedEvent.AggregateType, rejectedEvent.AggregateID, rejectedEvent.EventType, payload)
			if err != nil {
				return nil, fmt.Errorf("insert rejected outbox event: %w", err)
			}

			if err := resTx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("commit reject: %w", err)
			}

			return nil, fmt.Errorf("%w: %w", ErrInsufficientBalance, err)
		}

		tx.SetInternalReference(ref.ID())
		tx.SetResultBalance(balAfter)

		err = s.repo.UpdateStatusTx(ctx, resTx, tx.ID(), PROCESSED, "")
		if err != nil {
			return nil, fmt.Errorf("update resolved status: %w", err)
		}
		err = s.repo.SetInternalReference(ctx, tx.ID(), ref.ID())
		if err != nil {
			return nil, fmt.Errorf("set internal reference: %w", err)
		}

		_, err = s.repo.CreateWalletLedgerEntryTx(ctx, resTx,
			tx.WalletID(), tx.ID(), string(dir), amount.Amount(), string(amount.Currency()),
			balBefore.Amount(), balAfter.Amount(),
		)
		if err != nil {
			return nil, fmt.Errorf("insert ledger entry: %w", err)
		}

		events := buildTransactionEvents(tx, dir, amount, balBefore, balAfter, walletVersion)
		for _, ev := range events {
			payload, _ := json.Marshal(ev)
			err = s.repo.CreateOutboxEventTx(ctx, resTx, ev.AggregateType, ev.AggregateID, ev.EventType, payload)
			if err != nil {
				return nil, fmt.Errorf("insert outbox event: %w", err)
			}
		}

		if err := resTx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit resolve: %w", err)
		}

		return s.result(tx, balAfter)
	}

	return s.resolveRejectInPlace(ctx, tx, "REFERENCE_NOT_SUCCESSFUL")
}

func (s *Service) resolveRejectInPlace(ctx context.Context, tx *Transaction, failureCode string) (*ProcessResult, error) {
	tx.SetFailureCode(failureCode)
	tx.TransitionTo(REJECTED)

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reject-in-place transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	err = s.repo.UpdateStatusTx(ctx, dbTx, tx.ID(), REJECTED, failureCode)
	if err != nil {
		return nil, fmt.Errorf("update rejected status: %w", err)
	}

	rejectedEvent := domain.Event{
		EventID:       fmt.Sprintf("evt-%s-rejected", tx.ID()),
		CorrelationID: tx.ID(),
		EventType:     "WagerTransactionRejected",
		AggregateType: "WagerTransaction",
		AggregateID:   tx.ID(),
		OccurredAt:    tx.UpdatedAt(),
		Version:       1,
		Data: domain.WagerTransactionRejectedData{
			TransactionID: tx.ID(),
			ExternalID:    tx.ExternalID(),
			ProviderID:    tx.Provider(),
			PlayerID:      tx.PlayerID(),
			WalletID:      tx.WalletID(),
			Kind:          string(tx.TransactionType()),
			Money:         tx.Amount(),
			FailureCode:   failureCode,
		},
	}
	payload, _ := json.Marshal(rejectedEvent)
	err = s.repo.CreateOutboxEventTx(ctx, dbTx, rejectedEvent.AggregateType, rejectedEvent.AggregateID, rejectedEvent.EventType, payload)
	if err != nil {
		return nil, fmt.Errorf("insert rejected outbox event: %w", err)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reject: %w", err)
	}

	w, _ := s.getWalletBalance(ctx, tx.WalletID())
	return &ProcessResult{
		TransactionID: tx.ID(),
		Status:        string(REJECTED),
		Balance:       w,
	}, nil
}

func (s *Service) createAndReject(ctx context.Context, tx *Transaction, failureCode string) error {
	tx.SetFailureCode(failureCode)
	tx.TransitionTo(REJECTED)

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create-reject transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
	if err != nil {
		return fmt.Errorf("insert rejected transaction: %w", err)
	}
	tx.SetID(txID)

	rejectedEvent := domain.Event{
		EventID:       fmt.Sprintf("evt-%s-rejected", txID),
		CorrelationID: txID,
		EventType:     "WagerTransactionRejected",
		AggregateType: "WagerTransaction",
		AggregateID:   txID,
		OccurredAt:    tx.UpdatedAt(),
		Version:       1,
		Data: domain.WagerTransactionRejectedData{
			TransactionID: txID,
			ExternalID:    tx.ExternalID(),
			ProviderID:    tx.Provider(),
			PlayerID:      tx.PlayerID(),
			WalletID:      tx.WalletID(),
			Kind:          string(tx.TransactionType()),
			Money:         tx.Amount(),
			FailureCode:   failureCode,
		},
	}
	payload, _ := json.Marshal(rejectedEvent)
	err = s.repo.CreateOutboxEventTx(ctx, dbTx, rejectedEvent.AggregateType, rejectedEvent.AggregateID, rejectedEvent.EventType, payload)
	if err != nil {
		return fmt.Errorf("insert rejected outbox event: %w", err)
	}

	return dbTx.Commit(ctx)
}

func (s *Service) reject(ctx context.Context, tx *Transaction, failureCode string) (*ProcessResult, error) {
	tx.SetFailureCode(failureCode)
	tx.TransitionTo(REJECTED)

	dbTx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reject transaction: %w", err)
	}
	defer dbTx.Rollback(ctx)

	txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
	if err != nil {
		return nil, fmt.Errorf("insert rejected transaction: %w", err)
	}
	tx.SetID(txID)

	rejectedEvent := domain.Event{
		EventID:       fmt.Sprintf("evt-%s-rejected", txID),
		CorrelationID: txID,
		EventType:     "WagerTransactionRejected",
		AggregateType: "WagerTransaction",
		AggregateID:   txID,
		OccurredAt:    tx.UpdatedAt(),
		Version:       1,
		Data: domain.WagerTransactionRejectedData{
			TransactionID: txID,
			ExternalID:    tx.ExternalID(),
			ProviderID:    tx.Provider(),
			PlayerID:      tx.PlayerID(),
			WalletID:      tx.WalletID(),
			Kind:          string(tx.TransactionType()),
			Money:         tx.Amount(),
			FailureCode:   failureCode,
		},
	}
	payload, _ := json.Marshal(rejectedEvent)
	err = s.repo.CreateOutboxEventTx(ctx, dbTx, rejectedEvent.AggregateType, rejectedEvent.AggregateID, rejectedEvent.EventType, payload)
	if err != nil {
		return nil, fmt.Errorf("insert rejected outbox event: %w", err)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reject: %w", err)
	}

	w, _ := s.getWalletBalance(ctx, tx.WalletID())
	return &ProcessResult{
		TransactionID: tx.ID(),
		Status:        string(REJECTED),
		Balance:       w,
	}, nil
}

func (s *Service) commitWithLedger(ctx context.Context, dbTx domain.DBTx, tx *Transaction, dir domain.Direction, amount money.Money, balBefore, balAfter money.Money, walletVersion int64, messageID string) error {

	if messageID != "" {
		if _, err := s.inboxRepo.RecordReceivedTx(ctx, dbTx, consumerName, messageID, ""); err != nil {
			return fmt.Errorf("record inbox: %w", err)
		}
	}

	tx.TransitionTo(PROCESSED)

	txID, err := s.repo.CreateTransactionTx(ctx, dbTx, tx)
	if err != nil {
		if isUniqueViolation(err) {
			if (tx.TransactionType() == REFUND || tx.TransactionType() == ROLLBACK) && tx.InternalReference() != "" {
				_ = dbTx.Rollback(ctx)
				return ErrDoubleReversal
			}
			existing, findErr := s.repo.FindByIdempotencyKey(ctx, tx.Provider(), tx.IdempotencyKey())
			if findErr == nil && existing != nil {
				_ = dbTx.Rollback(ctx)
				return ErrIdempotentDuplicate
			}
		}
		return fmt.Errorf("insert transaction: %w", err)
	}
	tx.SetID(txID)

	_, err = s.repo.CreateWalletLedgerEntryTx(ctx, dbTx,
		tx.WalletID(), txID, string(dir), amount.Amount(), string(amount.Currency()),
		balBefore.Amount(), balAfter.Amount(),
	)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}

	events := buildTransactionEvents(tx, dir, amount, balBefore, balAfter, walletVersion)
	for _, ev := range events {
		payload, _ := json.Marshal(ev)
		err = s.repo.CreateOutboxEventTx(ctx, dbTx, ev.AggregateType, ev.AggregateID, ev.EventType, payload)
		if err != nil {
			return fmt.Errorf("insert outbox event: %w", err)
		}
	}

	if messageID != "" {
		_ = s.inboxRepo.MarkCompletedTx(ctx, dbTx, consumerName, messageID)
	}

	if err := dbTx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *Service) getWalletBalance(ctx context.Context, walletID string) (money.Money, error) {
	w, err := s.walletSvc.GetBalance(ctx, walletID)
	if err != nil {
		return money.Money{}, fmt.Errorf("get wallet balance: %w", err)
	}
	return w, nil
}

func (s *Service) result(tx *Transaction, balance money.Money) (*ProcessResult, error) {
	return &ProcessResult{
		TransactionID: tx.ID(),
		Status:        string(tx.Status()),
		Balance:       balance,
	}, nil
}

func (s *Service) validateReference(tx *Transaction, ref *Transaction) error {
	if ref.Provider() != tx.Provider() {
		return ErrProviderMismatch
	}
	if ref.PlayerID() != tx.PlayerID() {
		return ErrPlayerMismatch
	}
	if ref.WalletID() != tx.WalletID() {
		return ErrWalletMismatch
	}
	if ref.Amount().Currency() != tx.Amount().Currency() {
		return ErrCurrencyMismatch
	}
	if ref.RoundID() != tx.RoundID() {
		return ErrRoundMismatch
	}
	return nil
}

func (s *Service) validateReversalValue(amount money.Money, ref *Transaction) error {
	cmp, _ := amount.Compare(ref.Amount())
	if cmp != 0 {
		return ErrReversalValueMismatch
	}
	return nil
}

func mapWalletError(err error) string {
	errMsg := err.Error()
	if errors.Is(err, wallet.ErrInsufficientBalance) || errMsg == "insufficient_balance" {
		return "INSUFFICIENT_BALANCE"
	}
	if errors.Is(err, wallet.ErrCurrencyMismatch) || errMsg == "currency_mismatch" {
		return "CURRENCY_MISMATCH"
	}
	return "WALLET_ERROR"
}

func mapWalletErrorForReversal(err error) string {
	errMsg := err.Error()
	if errors.Is(err, wallet.ErrInsufficientBalance) || errMsg == "insufficient_balance" {
		return "REVERSAL_INSUFFICIENT_BALANCE"
	}
	if errors.Is(err, wallet.ErrCurrencyMismatch) || errMsg == "currency_mismatch" {
		return "CURRENCY_MISMATCH"
	}
	return "WALLET_ERROR"
}

func mapValidationError(err error) string {
	switch err {
	case ErrProviderMismatch:
		return "PROVIDER_MISMATCH"
	case ErrPlayerMismatch:
		return "PLAYER_MISMATCH"
	case ErrWalletMismatch:
		return "WALLET_MISMATCH"
	case ErrCurrencyMismatch:
		return "CURRENCY_MISMATCH"
	case ErrRoundMismatch:
		return "ROUND_MISMATCH"
	default:
		return "VALIDATION_ERROR"
	}
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func buildTransactionEvents(tx *Transaction, dir domain.Direction, amount money.Money, balBefore, balAfter money.Money, walletVersion int64) []domain.Event {
	var events []domain.Event

	events = append(events, domain.Event{
		EventID:       fmt.Sprintf("evt-%s-processed", tx.ID()),
		CorrelationID: tx.ID(),
		EventType:     "WagerTransactionProcessed",
		AggregateType: "WagerTransaction",
		AggregateID:   tx.ID(),
		OccurredAt:    tx.UpdatedAt(),
		Version:       1,
		Data: domain.WagerTransactionProcessedData{
			TransactionID:  tx.ID(),
			ExternalID:     tx.ExternalID(),
			ProviderID:     tx.Provider(),
			PlayerID:       tx.PlayerID(),
			WalletID:       tx.WalletID(),
			RoundID:        tx.RoundID(),
			GameID:         tx.GameID(),
			Kind:           string(tx.TransactionType()),
			Money:          tx.Amount(),
			ReferenceExtID: tx.ExternalReference(),
			Status:         string(tx.Status()),
		},
	})

	if !amount.IsZero() {
		events = append(events, domain.Event{
			EventID:       fmt.Sprintf("evt-%s-balance", tx.ID()),
			CorrelationID: tx.ID(),
			EventType:     "WalletBalanceChanged",
			AggregateType: "Wallet",
			AggregateID:   tx.WalletID(),
			OccurredAt:    tx.UpdatedAt(),
			Version:       1,
			Data: domain.WalletBalanceChangedData{
				WalletID:      tx.WalletID(),
				TransactionID: tx.ID(),
				Direction:     string(dir),
				Money:         amount,
				BalanceBefore: balBefore,
				BalanceAfter:  balAfter,
				WalletVersion: walletVersion,
			},
		})
	}

	return events
}

type Request struct {
	ProviderID            string   `json:"providerId"`
	ExternalTransactionID string   `json:"externalTransactionId"`
	PlayerID              string   `json:"playerId"`
	WalletID              string   `json:"walletId"`
	RoundID               string   `json:"roundId"`
	GameID                string   `json:"gameId"`
	Kind                  string   `json:"kind"`
	Money                 MoneyDTO `json:"money"`
	ReferenceExternalID   string   `json:"referenceExternalTransactionId"`
	IdempotencyKey        string   `json:"idempotencyKey"`
}

type MoneyDTO struct {
	Amount   string
	Currency string
}

func isUniqueViolation(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique constraint")
}
