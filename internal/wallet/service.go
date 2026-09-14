package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/domain"
	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	repo       Repository
	ledgerRepo LedgerRepository
	pool       *pgxpool.Pool
}

func NewService(
	repo Repository,
	ledgerRepo LedgerRepository,
	pool *pgxpool.Pool,
) *Service {
	return &Service{
		repo:       repo,
		ledgerRepo: ledgerRepo,
		pool:       pool,
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

	if s.pool == nil {
		return nil, fmt.Errorf("create wallet: database pool is nil")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var walletID string

	err = tx.QueryRow(
		ctx,
		`
		INSERT INTO wallets (
			player_id,
			provider_id,
			currency,
			balance,
			version,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, 1, now(), now())
		RETURNING id
		`,
		playerID,
		providerID,
		string(currency),
		initialBalance.Amount(),
	).Scan(&walletID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateWallet
		}

		return nil, fmt.Errorf("insert wallet: %w", err)
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
		var openingID string

		err = tx.QueryRow(
			ctx,
			`
			INSERT INTO wager_transactions (
				origin,
				wallet_id,
				player_id,
				transaction_type,
				amount,
				currency,
				status,
				created_at,
				updated_at,
				processed_at
			)
			VALUES (
				'INTERNAL',
				$1,
				$2,
				'OPENING',
				$3,
				$4,
				'PROCESSED',
				now(),
				now(),
				now()
			)
			RETURNING id
			`,
			walletID,
			playerID,
			initialBalance.Amount(),
			string(currency),
		).Scan(&openingID)
		if err != nil {
			return nil, fmt.Errorf("insert opening transaction: %w", err)
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

		var entryID string

		err = tx.QueryRow(
			ctx,
			`
			INSERT INTO wallet_ledger_entries (
				wallet_id,
				transaction_id,
				direction,
				amount,
				currency,
				balance_before,
				balance_after,
				created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			RETURNING id
			`,
			walletID,
			openingID,
			string(entry.Direction()),
			entry.Amount().Amount(),
			string(entry.Currency()),
			entry.BalanceBefore().Amount(),
			entry.BalanceAfter().Amount(),
		).Scan(&entryID)
		if err != nil {
			return nil, fmt.Errorf("insert ledger entry: %w", err)
		}

		events := buildWalletCreatedEvents(w, openingID, entry)

		for _, event := range events {
			payload, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("marshal outbox event: %w", err)
			}

			_, err = tx.Exec(
				ctx,
				`
				INSERT INTO outbox_events (
					aggregate_type,
					aggregate_id,
					event_type,
					payload,
					occurred_at,
					attempts,
					next_attempt_at
				)
				VALUES ($1, $2, $3, $4, now(), 0, now())
				`,
				event.AggregateType,
				event.AggregateID,
				event.EventType,
				payload,
			)
			if err != nil {
				return nil, fmt.Errorf("insert outbox event: %w", err)
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
	dbTx pgx.Tx,
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
	dbTx pgx.Tx,
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
	dbTx pgx.Tx,
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

	if s.pool == nil && dbTx == nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("change wallet balance: database pool is nil")
	}

	tx := dbTx
	ownsTransaction := false

	if tx == nil {
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return money.Money{}, money.Money{}, 0,
				fmt.Errorf("begin balance transaction: %w", err)
		}

		ownsTransaction = true
		defer tx.Rollback(ctx)
	}

	var (
		idVal       string
		playerID    string
		providerID  string
		currencyStr string
		balance     int64
		version     int64
	)

	err = tx.QueryRow(
		ctx,
		`
		SELECT
			id,
			player_id,
			provider_id,
			currency,
			balance,
			version
		FROM wallets
		WHERE id = $1
		FOR UPDATE
		`,
		walletID,
	).Scan(
		&idVal,
		&playerID,
		&providerID,
		&currencyStr,
		&balance,
		&version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return money.Money{}, money.Money{}, 0,
				ErrWalletNotFound
		}

		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("load wallet for balance change: %w", err)
	}

	currency := money.Currency(currencyStr)

	currentBalance, err := money.NewMoney(balance, currency)
	if err != nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("parse wallet balance: %w", err)
	}

	w, err := RehydrateWallet(
		idVal,
		playerID,
		providerID,
		currency,
		currentBalance,
		version,
		time.Time{},
		time.Time{},
	)
	if err != nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("rehydrate wallet: %w", err)
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

	commandTag, err := tx.Exec(
		ctx,
		`
		UPDATE wallets
		SET
			balance = $1,
			version = $2,
			updated_at = now()
		WHERE id = $3
		  AND version = $4
		`,
		change.BalanceAfter.Amount(),
		change.NewVersion,
		idVal,
		version,
	)
	if err != nil {
		return money.Money{}, money.Money{}, 0,
			fmt.Errorf("update wallet balance: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return money.Money{}, money.Money{}, 0,
			ErrConcurrentUpdate
	}

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
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
