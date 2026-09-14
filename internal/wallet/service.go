package wallet

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
)

type Service struct {
	repo       Repository
	ledgerRepo LedgerRepository
	txFactory  domain.DBTxFactory
	metrics    *metrics.Metrics
}

func NewService(
	repo Repository,
	ledgerRepo LedgerRepository,
	txFactory domain.DBTxFactory,
	m *metrics.Metrics,
) *Service {
	return &Service{
		repo:       repo,
		ledgerRepo: ledgerRepo,
		txFactory:  txFactory,
		metrics:    m,
	}
}

func (s *Service) CreateWallet(
	ctx context.Context,
	providerID string,
	playerID string,
	initialBalance money.Money,
) (*Wallet, error) {
	if providerID == "" {
		return nil, ErrInvalidProviderID
	}

	if playerID == "" {
		return nil, ErrInvalidPlayerID
	}

	if initialBalance.Currency() == "" {
		return nil, ErrInvalidCurrency
	}

	if initialBalance.IsNegative() {
		return nil, ErrNegativeBalance
	}

	currency := initialBalance.Currency()

	existing, err := s.repo.FindByPlayerAndCurrencyAndProvider(
		ctx,
		playerID,
		currency,
		providerID,
	)
	if err == nil && existing != nil {
		return nil, ErrDuplicateWallet
	}

	if err != nil && !errors.Is(err, ErrWalletNotFound) {
		return nil, fmt.Errorf("check existing wallet: %w", err)
	}

	if s.txFactory == nil {
		return nil, fmt.Errorf("create wallet: tx factory is nil")
	}

	tx, err := s.txFactory.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	walletID, err := s.repo.CreateWalletTx(ctx, tx, playerID, providerID, currency, initialBalance)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateWallet
		}
		return nil, err
	}

	now := time.Now().UTC()

	w, err := RehydrateWallet(
		walletID,
		playerID,
		providerID,
		currency,
		initialBalance,
		1,
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("rehydrate wallet: %w", err)
	}

	if !initialBalance.IsZero() {
		openingID, err := s.repo.CreateOpeningTx(ctx, tx, walletID, playerID, initialBalance, currency)
		if err != nil {
			return nil, err
		}

		zeroBalance, err := money.NewMoney(0, currency)
		if err != nil {
			return nil, fmt.Errorf("create zero balance: %w", err)
		}

		entry, err := NewLedgerEntry(
			"",
			walletID,
			openingID,
			CREDIT,
			initialBalance,
			zeroBalance,
			initialBalance,
		)
		if err != nil {
			return nil, fmt.Errorf("create ledger entry: %w", err)
		}

		_, err = s.repo.CreateLedgerEntryTx(ctx, tx, entry)
		if err != nil {
			return nil, err
		}

		events := buildWalletCreatedEvents(w, openingID, entry)

		for _, event := range events {
			payload, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("marshal outbox event: %w", err)
			}

			err = s.repo.CreateOutboxEventTx(ctx, tx, event.AggregateType, event.AggregateID, event.EventType, payload)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit wallet creation: %w", err)
	}

	return w, nil
}

func (s *Service) GetWallet(
	ctx context.Context,
	id string,
) (*Wallet, error) {
	if id == "" {
		return nil, ErrInvalidWalletID
	}

	return s.repo.FindByID(ctx, id)
}

func (s *Service) GetLedger(
	ctx context.Context,
	walletID string,
	cursor string,
	limit int,
) ([]*LedgerEntry, string, error) {
	if walletID == "" {
		return nil, "", ErrInvalidWalletID
	}

	if limit <= 0 {
		limit = 100
	}

	return s.ledgerRepo.FindByWalletID(
		ctx,
		walletID,
		cursor,
		limit,
	)
}

func (s *Service) Debit(
	ctx context.Context,
	dbTx domain.DBTx,
	walletID string,
	amount money.Money,
) (
	balanceBefore money.Money,
	balanceAfter money.Money,
	walletVersion int64,
	err error,
) {
	return s.applyBalanceChange(
		ctx,
		dbTx,
		walletID,
		amount,
		DEBIT,
	)
}

func (s *Service) Credit(
	ctx context.Context,
	dbTx domain.DBTx,
	walletID string,
	amount money.Money,
) (
	balanceBefore money.Money,
	balanceAfter money.Money,
	walletVersion int64,
	err error,
) {
	return s.applyBalanceChange(
		ctx,
		dbTx,
		walletID,
		amount,
		CREDIT,
	)
}

func (s *Service) applyBalanceChange(
	ctx context.Context,
	dbTx domain.DBTx,
	walletID string,
	amount money.Money,
	direction Direction,
) (
	balanceBefore money.Money,
	balanceAfter money.Money,
	walletVersion int64,
	err error,
) {
	if walletID == "" {
		return money.Money{}, money.Money{}, 0, ErrInvalidWalletID
	}

	if direction != DEBIT && direction != CREDIT {
		return money.Money{}, money.Money{}, 0, ErrInvalidLedgerEntry
	}

	if s.txFactory == nil && dbTx == nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("change wallet balance: tx factory is nil")
	}

	tx := dbTx
	ownsTransaction := false

	if tx == nil {
		tx, err = s.txFactory.Begin(ctx)
		if err != nil {
			return money.Money{}, money.Money{}, 0,
				fmt.Errorf("begin balance transaction: %w", err)
		}

		ownsTransaction = true
		defer tx.Rollback(ctx)
	}

	w, err := s.repo.FindByIDForUpdate(ctx, tx, walletID)
	if err != nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("load wallet for balance change: %w", err)
	}

	var change *BalanceChange

	switch direction {
	case DEBIT:
		change, err = w.Debit(amount)
	case CREDIT:
		change, err = w.Credit(amount)
	}
	if err != nil {
		return money.Money{}, money.Money{}, 0, err
	}

	newVersion, err := s.repo.UpdateBalanceTx(ctx, tx, w.ID(), change.BalanceAfter, w.Version())
	if err != nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("update wallet balance: %w", err)
	}

	_ = newVersion

	if ownsTransaction {
		if err := tx.Commit(ctx); err != nil {
			return money.Money{}, money.Money{}, 0,
				fmt.Errorf("commit balance transaction: %w", err)
		}
	}

	return change.BalanceBefore, change.BalanceAfter, change.NewVersion, nil
}

func (s *Service) Reconcile(
	ctx context.Context,
	walletID string,
) (*ReconciliationResult, error) {
	if walletID == "" {
		return nil, ErrInvalidWalletID
	}

	w, err := s.repo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	calculatedSum, err := s.ledgerRepo.SumByWalletID(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("sum wallet ledger: %w", err)
	}

	checkedEntries := 0
	cursor := ""

	for {
		entries, nextCursor, err := s.ledgerRepo.FindByWalletID(
			ctx,
			walletID,
			cursor,
			1000,
		)
		if err != nil {
			return nil, fmt.Errorf("read wallet ledger: %w", err)
		}

		checkedEntries += len(entries)

		if nextCursor == "" || nextCursor == cursor {
			break
		}

		cursor = nextCursor
	}

	calculated, err := money.NewMoney(
		calculatedSum,
		w.Currency(),
	)
	if err != nil {
		return nil, fmt.Errorf("parse calculated balance: %w", err)
	}

	stored := w.Balance()

	difference, err := stored.Subtract(calculated)
	if err != nil {
		return nil, fmt.Errorf("calculate balance difference: %w", err)
	}

	if !difference.IsZero() && s.metrics != nil {
		s.metrics.ReconciliationDiv.Inc()
	}

	return &ReconciliationResult{
		WalletID:          walletID,
		StoredBalance:     stored,
		CalculatedBalance: calculated,
		Difference:        difference,
		Consistent:        difference.IsZero(),
		CheckedEntries:    checkedEntries,
	}, nil
}

type ReconciliationResult struct {
	WalletID          string
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}

func buildWalletCreatedEvents(
	w *Wallet,
	openingID string,
	entry *LedgerEntry,
) []domain.Event {
	return []domain.Event{
		{
			EventID:       fmt.Sprintf("evt-%s-processed", openingID),
			EventType:     "WagerTransactionProcessed",
			AggregateType: "WagerTransaction",
			AggregateID:   openingID,
			OccurredAt:    w.CreatedAt(),
			Version:       1,
			Data: domain.WagerTransactionProcessedData{
				TransactionID: openingID,
				PlayerID:      w.PlayerID(),
				WalletID:      w.ID(),
				Kind:          "OPENING",
				Money:         w.Balance(),
				Status:        "PROCESSED",
			},
		},
		{
			EventID:       fmt.Sprintf("evt-%s-balance", openingID),
			EventType:     "WalletBalanceChanged",
			AggregateType: "Wallet",
			AggregateID:   w.ID(),
			OccurredAt:    w.CreatedAt(),
			Version:       1,
			Data: domain.WalletBalanceChangedData{
				WalletID:      w.ID(),
				TransactionID: openingID,
				Direction:     string(CREDIT),
				Money:         w.Balance(),
				BalanceBefore: entry.BalanceBefore(),
				BalanceAfter:  entry.BalanceAfter(),
				WalletVersion: w.Version(),
			},
		},
	}
}

func isUniqueViolation(err error) bool {
	errMsg := err.Error()
	return strings.Contains(errMsg, "duplicate key") || strings.Contains(errMsg, "unique constraint")
}
