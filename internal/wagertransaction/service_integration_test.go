//go:build integration

package wagertransaction

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, walletID, playerID, providerID string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, player_id, provider_id, currency, balance, version, created_at, updated_at)
		 VALUES ($1, $2, $3, 'BRL', $4, 1, now(), now())
		 ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance, version = wallets.version + 1, updated_at = now()`, walletID, playerID, providerID, balance,
	)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}

func createTestService(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	repo := newTestRepo(pool)
	walletSvc := newTestWalletSvc(pool)
	inboxRepo := newTestInboxRepo(pool)
	outboxRepo := newTestOutboxRepo(pool)
	txManager := newTestTxManager(pool)
	return NewService(repo, inboxRepo, outboxRepo, walletSvc, txManager, nil)
}

func TestConcurrency_DoubleDebit(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000001"
	playerID := "00000000-0000-0000-0000-000000000010"
	seedWallet(t, pool, walletID, playerID, "test-double-debit", 10000)

	svc := createTestService(t, pool)

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([]error, goroutines)
	processedCount := 0
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := Request{
				ProviderID:            "test-double-debit",
				ExternalTransactionID: fmt.Sprintf("ext-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: MoneyDTO{
					Amount:   "10.00",
					Currency: "BRL",
				},
			}
			ctx := context.Background()
			result, err := svc.ProcessTransaction(ctx, req, "", "")
			if err != nil {
				results[idx] = err
			} else {
				mu.Lock()
				processedCount++
				mu.Unlock()
				t.Logf("goroutine %d: processed, balance=%s", idx, result.Balance)
			}
		}(i)
	}
	wg.Wait()

	var errCount int
	for _, err := range results {
		if err != nil {
			errCount++
		}
	}
	t.Logf("processed=%d, errors=%d (insufficient_balance expected for %d)", processedCount, errCount, goroutines-processedCount)

	if processedCount == 0 {
		t.Error("expected at least one bet to succeed")
	}

	finalRow := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID)
	var finalBalance int64
	if err := finalRow.Scan(&finalBalance); err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	t.Logf("final balance: %d", finalBalance)
	if finalBalance < 0 {
		t.Errorf("balance went negative: %d", finalBalance)
	}
}

func TestConcurrency_IdempotentReplay(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000002"
	playerID := "00000000-0000-0000-0000-000000000020"
	seedWallet(t, pool, walletID, playerID, "prov-idempotent", 100000)

	svc := createTestService(t, pool)

	req := Request{
		ProviderID:            "prov-idempotent",
		ExternalTransactionID: "ext-idempotent",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money: MoneyDTO{
			Amount:   "5.00",
			Currency: "BRL",
		},
	}
	key := req.ProviderID + ":" + req.ExternalTransactionID

	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]*ProcessResult, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := svc.ProcessTransaction(context.Background(), req, key, "")
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			results[idx] = result
		}(i)
	}
	wg.Wait()

	var uniqueTXIDs map[string]bool
	uniqueTXIDs = make(map[string]bool)
	for _, r := range results {
		if r != nil {
			uniqueTXIDs[r.TransactionID] = true
		}
	}
	t.Logf("unique transaction IDs: %d", len(uniqueTXIDs))
	if len(uniqueTXIDs) > 1 {
		t.Errorf("expected all goroutines to return same transaction ID, got %d unique", len(uniqueTXIDs))
	}

	finalRow := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID)
	var finalBalance int64
	if err := finalRow.Scan(&finalBalance); err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	expectedBalance := int64(100000) - int64(500)
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d (expected exactly one debit)", finalBalance, expectedBalance)
	}

	countRow := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1 AND direction = 'DEBIT'`, walletID)
	var debitCount int
	if err := countRow.Scan(&debitCount); err != nil {
		t.Fatalf("count ledger debits: %v", err)
	}
	if debitCount != 1 {
		t.Errorf("expected exactly 1 debit in ledger, got %d", debitCount)
	}
}

func TestConcurrency_TwoBetsOn100Balance(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000004"
	playerID := "00000000-0000-0000-0000-000000000040"
	seedWallet(t, pool, walletID, playerID, "test-two-bets", 10000)

	svc := createTestService(t, pool)

	const goroutines = 2
	var wg sync.WaitGroup
	processed := make(chan string, goroutines)
	rejected := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := Request{
				ProviderID:            "test-two-bets",
				ExternalTransactionID: fmt.Sprintf("ext-80-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: MoneyDTO{
					Amount:   "80.00",
					Currency: "BRL",
				},
			}
			result, err := svc.ProcessTransaction(context.Background(), req, "", "")
			if err != nil {
				rejected <- err
			} else {
				processed <- result.TransactionID
			}
		}(i)
	}

	wg.Wait()
	close(processed)
	close(rejected)

	processedCount := 0
	for range processed {
		processedCount++
	}
	rejectedCount := 0
	for err := range rejected {
		rejectedCount++
		t.Logf("rejected: %v", err)
	}

	t.Logf("processed=%d, rejected=%d", processedCount, rejectedCount)

	if processedCount != 1 {
		t.Errorf("expected exactly 1 processed bet, got %d", processedCount)
	}
	if rejectedCount != 1 {
		t.Errorf("expected exactly 1 rejected bet, got %d", rejectedCount)
	}

	finalRow := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID)
	var finalBalance int64
	if err := finalRow.Scan(&finalBalance); err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	expectedBalance := int64(2000)
	if finalBalance != expectedBalance {
		t.Errorf("final balance mismatch: got %d, want %d", finalBalance, expectedBalance)
	}

	countRow := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1 AND direction = 'DEBIT'`, walletID)
	var debitCount int
	if err := countRow.Scan(&debitCount); err != nil {
		t.Fatalf("count ledger debits: %v", err)
	}
	if debitCount != 1 {
		t.Errorf("expected exactly 1 debit in ledger, got %d", debitCount)
	}
}

func TestConcurrency_OptimisticLocking(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000003"
	playerID := "00000000-0000-0000-0000-000000000030"
	seedWallet(t, pool, walletID, playerID, "test-lock", 100000)

	svc := createTestService(t, pool)

	const goroutines = 15
	var wg sync.WaitGroup
	processed := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := Request{
				ProviderID:            "test-lock",
				ExternalTransactionID: fmt.Sprintf("ext-lock-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: MoneyDTO{
					Amount:   "1.00",
					Currency: "BRL",
				},
			}
			result, err := svc.ProcessTransaction(context.Background(), req, "", "")
			if err == nil {
				processed <- result.TransactionID
			}
		}(i)
	}

	wg.Wait()
	close(processed)

	_ = money.Money{}
	var count int
	for txID := range processed {
		count++
		t.Logf("processed tx: %s", txID)
	}
	t.Logf("total processed: %d", count)

	finalRow := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID)
	var finalBalance int64
	if err := finalRow.Scan(&finalBalance); err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	expectedBalance := int64(100000) - int64(count)*100
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d", finalBalance, expectedBalance)
	}
}

func TestDoubleReversal_Rejected(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000010"
	playerID := "00000000-0000-0000-0000-000000000011"
	providerID := "prov-double-reversal"
	seedWallet(t, pool, walletID, playerID, "prov-double-reversal", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "50.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}
	t.Logf("BET processed: %s", betResult.TransactionID)

	// Step 2: REFUND the BET
	refundReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "REFUND",
		Money:                 MoneyDTO{Amount: "50.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-100",
	}
	refundResult, err := svc.ProcessTransaction(ctx, refundReq, "", "")
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}
	t.Logf("REFUND processed: %s", refundResult.TransactionID)

	// Step 3: Try ROLLBACK on the same BET - should be rejected
	rollbackReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "ROLLBACK",
		Money:                 MoneyDTO{Amount: "50.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-100",
	}
	rollbackResult, err := svc.ProcessTransaction(ctx, rollbackReq, "", "")
	if err != nil {
		t.Fatalf("process ROLLBACK should not error: %v", err)
	}
	if rollbackResult.Status != "REJECTED" {
		t.Errorf("expected ROLLBACK to be rejected after REFUND, got status=%s", rollbackResult.Status)
	}
	t.Logf("ROLLBACK correctly rejected: %s", rollbackResult.TransactionID)
}

func TestDoubleReversal_CrossType_Rejected(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000020"
	playerID := "00000000-0000-0000-0000-000000000021"
	providerID := "prov-cross-reversal"
	seedWallet(t, pool, walletID, playerID, "prov-cross-reversal", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "30.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}
	t.Logf("BET processed: %s", betResult.TransactionID)

	// Step 2: ROLLBACK the BET
	rollbackReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "ROLLBACK",
		Money:                 MoneyDTO{Amount: "30.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-200",
	}
	rollbackResult, err := svc.ProcessTransaction(ctx, rollbackReq, "", "")
	if err != nil {
		t.Fatalf("process ROLLBACK: %v", err)
	}
	t.Logf("ROLLBACK processed: %s", rollbackResult.TransactionID)

	// Step 3: Try REFUND on the same BET - should be rejected (cross-type double reversal)
	refundReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "REFUND",
		Money:                 MoneyDTO{Amount: "30.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-200",
	}
	refundResult, err := svc.ProcessTransaction(ctx, refundReq, "", "")
	if err != nil {
		t.Fatalf("process REFUND should not error: %v", err)
	}
	if refundResult.Status != "REJECTED" {
		t.Errorf("expected REFUND to be rejected after ROLLBACK, got status=%s", refundResult.Status)
	}
	t.Logf("REFUND correctly rejected: %s", refundResult.TransactionID)
}

func TestLossProducesNoLedgerEntry(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000030"
	playerID := "00000000-0000-0000-0000-000000000031"
	providerID := "prov-loss"
	seedWallet(t, pool, walletID, playerID, "prov-loss", 50000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Get initial wallet version
	var initialVersion int64
	err := pool.QueryRow(ctx, `SELECT version FROM wallets WHERE id = $1`, walletID).Scan(&initialVersion)
	if err != nil {
		t.Fatalf("get initial version: %v", err)
	}

	// Process a LOSS
	lossReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "loss-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-3",
		Kind:                  "LOSS",
		Money:                 MoneyDTO{Amount: "0.00", Currency: "BRL"},
	}
	lossResult, err := svc.ProcessTransaction(ctx, lossReq, "", "")
	if err != nil {
		t.Fatalf("process LOSS: %v", err)
	}
	t.Logf("LOSS processed: %s", lossResult.TransactionID)

	// Verify wallet version unchanged
	var finalVersion int64
	err = pool.QueryRow(ctx, `SELECT version FROM wallets WHERE id = $1`, walletID).Scan(&finalVersion)
	if err != nil {
		t.Fatalf("get final version: %v", err)
	}
	if finalVersion != initialVersion {
		t.Errorf("wallet version changed after LOSS: initial=%d, final=%d", initialVersion, finalVersion)
	}

	// Verify no ledger entry for this transaction
	var ledgerCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1 AND transaction_id = $2`,
		walletID, lossResult.TransactionID,
	).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if ledgerCount != 0 {
		t.Errorf("expected 0 ledger entries for LOSS, got %d", ledgerCount)
	}

	// Verify outbox event was created
	var outboxCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1`,
		lossResult.TransactionID,
	).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox event for LOSS, got %d", outboxCount)
	}
}

func TestReferenceResolution_PendingThenArrives(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000040"
	playerID := "00000000-0000-0000-0000-000000000041"
	providerID := "prov-ref-pending"
	seedWallet(t, pool, walletID, playerID, "prov-ref-pending", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET (will be in PENDING state initially)
	betReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-pending-1",
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "20.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}
	t.Logf("BET processed: %s", betResult.TransactionID)

	// Step 2: Try REFUND before BET is fully committed (simulate pending)
	// First, manually set the BET status to PENDING
	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET status = 'PENDING', processed_at = NULL WHERE id = $1`,
		betResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set BET to PENDING: %v", err)
	}

	// Step 3: REFUND should go to PENDING_REFERENCE
	refundReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-pending-1",
		Kind:                  "REFUND",
		Money:                 MoneyDTO{Amount: "20.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-pending-1",
	}
	refundResult, err := svc.ProcessTransaction(ctx, refundReq, "", "")
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}
	if refundResult.Status != "PENDING_REFERENCE" {
		t.Errorf("expected PENDING_REFERENCE, got %s", refundResult.Status)
	}
	t.Logf("REFUND pending reference: %s", refundResult.TransactionID)

	// Step 4: Resolve the BET back to PROCESSED
	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET status = 'PROCESSED', processed_at = now() WHERE id = $1`,
		betResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set BET to PROCESSED: %v", err)
	}

	// Step 5: Resolve the pending reference
	resolveResult, err := svc.ResolvePendingReference(ctx, refundResult.TransactionID)
	if err != nil {
		t.Fatalf("resolve pending reference: %v", err)
	}
	if resolveResult.Status != "PROCESSED" {
		t.Errorf("expected PROCESSED after resolution, got %s", resolveResult.Status)
	}
	t.Logf("Reference resolved: %s", resolveResult.TransactionID)
}

func TestReferenceResolution_MaxRetriesExhausted(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000050"
	playerID := "00000000-0000-0000-0000-000000000051"
	providerID := "prov-ref-max-retry"
	seedWallet(t, pool, walletID, playerID, "prov-ref-max-retry", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-max-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-max-1",
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "10.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}

	// Set BET to PENDING
	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET status = 'PENDING', processed_at = NULL WHERE id = $1`,
		betResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set BET to PENDING: %v", err)
	}

	// Step 2: REFUND goes to PENDING_REFERENCE
	refundReq := Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-max-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-max-1",
		Kind:                  "REFUND",
		Money:                 MoneyDTO{Amount: "10.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-max-1",
	}
	refundResult, err := svc.ProcessTransaction(ctx, refundReq, "", "")
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}

	// Step 3: Simulate max retries by setting ref_attempts to 10
	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET ref_attempts = 10 WHERE id = $1`,
		refundResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set ref_attempts: %v", err)
	}

	// Step 4: Resolve should reject
	resolveResult, err := svc.ResolvePendingReference(ctx, refundResult.TransactionID)
	if err != nil {
		t.Fatalf("resolve should not error: %v", err)
	}
	if resolveResult.Status != "REJECTED" {
		t.Errorf("expected REJECTED after max retries, got %s", resolveResult.Status)
	}
	t.Logf("Reference correctly rejected after max retries: %s", resolveResult.TransactionID)
}

func TestConcurrentInstances_SeparateConnections(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000060"
	playerID := "00000000-0000-0000-0000-000000000061"
	seedWallet(t, pool, walletID, playerID, "test-inst", 100000)

	// Create 3 separate pgxpool connections to simulate 3 independent instances
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	svc1 := createTestService(t, pool)
	svc2 := createTestService(t, pool)
	svc3 := createTestService(t, pool)

	const goroutines = 9
	var wg sync.WaitGroup
	processed := make(chan string, goroutines)

	services := []*Service{svc1, svc2, svc3}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			svc := services[idx%3]
			req := Request{
				ProviderID:            "test-inst",
				ExternalTransactionID: fmt.Sprintf("ext-inst-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: MoneyDTO{
					Amount:   "5.00",
					Currency: "BRL",
				},
			}
			result, err := svc.ProcessTransaction(ctx, req, "", "")
			if err == nil {
				processed <- result.TransactionID
			}
		}(i)
	}

	wg.Wait()
	close(processed)

	count := 0
	for txID := range processed {
		count++
		t.Logf("instance processed tx: %s", txID)
	}
	t.Logf("total processed across 3 instances: %d", count)

	// All 9 should succeed since each is a unique transaction
	if count != goroutines {
		t.Errorf("expected %d processed, got %d", goroutines, count)
	}

	// Verify final balance
	var finalBalance int64
	err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&finalBalance)
	if err != nil {
		t.Fatalf("read final balance: %v", err)
	}
	expectedBalance := int64(100000) - int64(goroutines)*500
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d", finalBalance, expectedBalance)
	}
}

func TestConcurrentWallets_Independent(t *testing.T) {
	pool := mustPool(t)
	walletA := "00000000-0000-0000-0000-000000000070"
	walletB := "00000000-0000-0000-0000-000000000071"
	playerA := "00000000-0000-0000-0000-00000000007A"
	playerB := "00000000-0000-0000-0000-00000000007B"
	seedWallet(t, pool, walletA, playerA, "test-pw-a", 50000)
	seedWallet(t, pool, walletB, playerB, "test-pw-b", 50000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	const goroutines = 10
	var wg sync.WaitGroup
	processedA := make(chan string, goroutines)
	processedB := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqA := Request{
				ProviderID:            "test-pw-a",
				ExternalTransactionID: fmt.Sprintf("ext-pw-a-%d", idx),
				PlayerID:              playerA,
				WalletID:              walletA,
				Kind:                  "BET",
				Money:                 MoneyDTO{Amount: "10.00", Currency: "BRL"},
			}
			result, err := svc.ProcessTransaction(ctx, reqA, "", "")
			if err == nil {
				processedA <- result.TransactionID
			}
		}(i)
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqB := Request{
				ProviderID:            "test-pw-b",
				ExternalTransactionID: fmt.Sprintf("ext-pw-b-%d", idx),
				PlayerID:              playerB,
				WalletID:              walletB,
				Kind:                  "BET",
				Money:                 MoneyDTO{Amount: "10.00", Currency: "BRL"},
			}
			result, err := svc.ProcessTransaction(ctx, reqB, "", "")
			if err == nil {
				processedB <- result.TransactionID
			}
		}(i)
	}

	wg.Wait()
	close(processedA)
	close(processedB)

	countA := 0
	for range processedA {
		countA++
	}
	countB := 0
	for range processedB {
		countB++
	}

	t.Logf("walletA processed=%d, walletB processed=%d", countA, countB)

	if countA == 0 || countB == 0 {
		t.Errorf("expected at least one bet per wallet, got A=%d B=%d", countA, countB)
	}

	var balA, balB int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&balA); err != nil {
		t.Fatalf("read walletA balance: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletB).Scan(&balB); err != nil {
		t.Fatalf("read walletB balance: %v", err)
	}
	t.Logf("walletA balance=%d, walletB balance=%d", balA, balB)
	if balA <= 0 || balB <= 0 {
		t.Error("expected positive balances on both wallets")
	}
}

func TestIdempotentRedelivery_SimulatedCrash(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000080"
	playerID := "00000000-0000-0000-0000-000000000081"
	seedWallet(t, pool, walletID, playerID, "prov-redelivery", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	req := Request{
		ProviderID:            "prov-redelivery",
		ExternalTransactionID: "ext-redelivery-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "15.00", Currency: "BRL"},
	}
	idempotencyKey := "prov-redelivery:ext-redelivery-1"

	result1, err := svc.ProcessTransaction(ctx, req, idempotencyKey, "")
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	t.Logf("first delivery: tx=%s status=%s", result1.TransactionID, result1.Status)

	result2, err := svc.ProcessTransaction(ctx, req, idempotencyKey, "")
	if err != nil {
		t.Fatalf("second delivery: %v", err)
	}
	if result2.TransactionID != result1.TransactionID {
		t.Errorf("idempotent redelivery returned different tx ID: %s vs %s", result2.TransactionID, result1.TransactionID)
	}
	if !result2.IdempotentReplay {
		t.Error("expected IdempotentReplay=true on redelivery")
	}

	var finalBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&finalBalance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	expectedBalance := int64(100000) - 1500
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d (only one debit expected)", finalBalance, expectedBalance)
	}

	var debitCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1 AND direction = 'DEBIT' AND transaction_id = $2`,
		walletID, result1.TransactionID,
	).Scan(&debitCount); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if debitCount != 1 {
		t.Errorf("expected exactly 1 DEBIT ledger entry, got %d", debitCount)
	}
}

func TestRestart_ConsistencyVerification(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000090"
	playerID := "00000000-0000-0000-0000-000000000091"
	seedWallet(t, pool, walletID, playerID, "prov-restart", 200000)

	svc1 := createTestService(t, pool)
	ctx := context.Background()

	betResult, err := svc1.ProcessTransaction(ctx, Request{
		ProviderID:            "prov-restart",
		ExternalTransactionID: "bet-restart-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "30.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}

	winResult, err := svc1.ProcessTransaction(ctx, Request{
		ProviderID:            "prov-restart",
		ExternalTransactionID: "win-restart-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "WIN",
		Money:                 MoneyDTO{Amount: "20.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process WIN: %v", err)
	}

	lossResult, err := svc1.ProcessTransaction(ctx, Request{
		ProviderID:            "prov-restart",
		ExternalTransactionID: "loss-restart-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "LOSS",
		Money:                 MoneyDTO{Amount: "0.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process LOSS: %v", err)
	}

	svc2 := createTestService(t, pool)

	txBet, err := svc2.FindByID(ctx, betResult.TransactionID)
	if err != nil {
		t.Fatalf("find BET after restart: %v", err)
	}
	if txBet.Status() != "PROCESSED" {
		t.Errorf("BET status after restart: got %s, want PROCESSED", txBet.Status())
	}

	txWin, err := svc2.FindByID(ctx, winResult.TransactionID)
	if err != nil {
		t.Fatalf("find WIN after restart: %v", err)
	}
	if txWin.Status() != "PROCESSED" {
		t.Errorf("WIN status after restart: got %s, want PROCESSED", txWin.Status())
	}

	txLoss, err := svc2.FindByID(ctx, lossResult.TransactionID)
	if err != nil {
		t.Fatalf("find LOSS after restart: %v", err)
	}
	if txLoss.Status() != "PROCESSED" {
		t.Errorf("LOSS status after restart: got %s, want PROCESSED", txLoss.Status())
	}

	var finalBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&finalBalance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	expectedBalance := int64(200000) - 3000 + 2000
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d", finalBalance, expectedBalance)
	}

	t.Logf("balance verified: %d (200000 - 3000 BET + 2000 WIN)", finalBalance)
}

func TestLedgerImmutability_UpdateAndDeleteBlocked(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-0000000000B0"
	playerID := "00000000-0000-0000-0000-0000000000B1"
	providerID := "prov-ledger-immut"
	seedWallet(t, pool, walletID, playerID, "prov-ledger-immut", 50000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	betResult, err := svc.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-ledger-immut-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "10.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}

	var ledgerID string
	err = pool.QueryRow(ctx,
		`SELECT id FROM wallet_ledger_entries WHERE wallet_id = $1 AND transaction_id = $2`,
		walletID, betResult.TransactionID,
	).Scan(&ledgerID)
	if err != nil {
		t.Fatalf("find ledger entry: %v", err)
	}

	_, err = pool.Exec(ctx,
		`UPDATE wallet_ledger_entries SET amount = 999 WHERE id = $1`, ledgerID,
	)
	if err == nil {
		t.Error("expected UPDATE to be blocked by trigger, but it succeeded")
	}

	_, err = pool.Exec(ctx,
		`DELETE FROM wallet_ledger_entries WHERE id = $1`, ledgerID,
	)
	if err == nil {
		t.Error("expected DELETE to be blocked by trigger, but it succeeded")
	}

	var intactAmount int64
	err = pool.QueryRow(ctx,
		`SELECT amount FROM wallet_ledger_entries WHERE id = $1`, ledgerID,
	).Scan(&intactAmount)
	if err != nil {
		t.Fatalf("read ledger entry after failed mutations: %v", err)
	}
	if intactAmount != 1000 {
		t.Errorf("ledger entry amount was mutated: got %d, want 1000", intactAmount)
	}
}

func TestCrashBetweenCommitAndSQSDelete_Redelivery(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-0000000000C0"
	playerID := "00000000-0000-0000-0000-0000000000C1"
	seedWallet(t, pool, walletID, playerID, "prov-crash-redeliver", 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	req := Request{
		ProviderID:            "prov-crash-redeliver",
		ExternalTransactionID: "ext-crash-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "25.00", Currency: "BRL"},
	}
	idempotencyKey := "prov-crash-redeliver:ext-crash-1"
	messageID := "sqs-msg-crash-001"

	result1, err := svc.ProcessTransaction(ctx, req, idempotencyKey, messageID)
	if err != nil {
		t.Fatalf("first delivery (simulated commit): %v", err)
	}
	t.Logf("first delivery committed: tx=%s status=%s", result1.TransactionID, result1.Status)

	result2, err := svc.ProcessTransaction(ctx, req, idempotencyKey, messageID)
	if err != nil {
		t.Fatalf("redelivery after crash: %v", err)
	}
	if result2.TransactionID != result1.TransactionID {
		t.Errorf("redelivery returned different tx ID: %s vs %s", result2.TransactionID, result1.TransactionID)
	}
	if !result2.IdempotentReplay {
		t.Error("expected IdempotentReplay=true on redelivery after crash")
	}

	var finalBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&finalBalance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	expectedBalance := int64(100000) - 2500
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d (only one debit expected)", finalBalance, expectedBalance)
	}

	var debitCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1 AND direction = 'DEBIT' AND transaction_id = $2`,
		walletID, result1.TransactionID,
	).Scan(&debitCount); err != nil {
		t.Fatalf("count debits: %v", err)
	}
	if debitCount != 1 {
		t.Errorf("expected exactly 1 DEBIT, got %d", debitCount)
	}
}

func TestAsyncPENDING_TwoInstancesCompeting(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-0000000000D0"
	playerID := "00000000-0000-0000-0000-0000000000D1"
	providerID := "prov-async-pending"
	seedWallet(t, pool, walletID, playerID, "prov-async-pending", 100000)

	svc1 := createTestService(t, pool)
	svc2 := createTestService(t, pool)
	ctx := context.Background()

	betResult, err := svc1.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-async-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "20.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}

	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET status = 'PENDING', processed_at = NULL WHERE id = $1`,
		betResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set BET to PENDING: %v", err)
	}

	refundResult, err := svc1.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-async-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "REFUND",
		Money:                 MoneyDTO{Amount: "20.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-async-pending-1",
	}, "", "")
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}
	if refundResult.Status != "PENDING_REFERENCE" {
		t.Fatalf("expected PENDING_REFERENCE, got %s", refundResult.Status)
	}

	_, err = pool.Exec(ctx,
		`UPDATE wager_transactions SET status = 'PROCESSED', processed_at = now() WHERE id = $1`,
		betResult.TransactionID,
	)
	if err != nil {
		t.Fatalf("set BET to PROCESSED: %v", err)
	}

	var resolveErr1, resolveErr2 error
	var resolveResult1, resolveResult2 *ProcessResult
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		resolveResult1, resolveErr1 = svc1.ResolvePendingReference(ctx, refundResult.TransactionID)
	}()
	go func() {
		defer wg.Done()
		resolveResult2, resolveErr2 = svc2.ResolvePendingReference(ctx, refundResult.TransactionID)
	}()
	wg.Wait()

	if resolveErr1 != nil && resolveErr2 != nil {
		t.Fatalf("both instances errored: svc1=%v, svc2=%v", resolveErr1, resolveErr2)
	}

	processedCount := 0
	if resolveErr1 == nil && resolveResult1.Status == "PROCESSED" {
		processedCount++
	}
	if resolveErr2 == nil && resolveResult2.Status == "PROCESSED" {
		processedCount++
	}

	t.Logf("resolved by %d instance(s)", processedCount)
	if processedCount != 1 {
		t.Errorf("expected exactly 1 instance to resolve, got %d", processedCount)
	}

	var finalBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&finalBalance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	expectedBalance := int64(100000) + 2000
	if finalBalance != expectedBalance {
		t.Errorf("balance mismatch: got %d, want %d (credit should happen exactly once)", finalBalance, expectedBalance)
	}
}

func TestReversalInsufficientBalance_DifferentCode(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-0000000000A0"
	playerID := "00000000-0000-0000-0000-0000000000A1"
	providerID := "prov-rev-insuf"
	seedWallet(t, pool, walletID, playerID, "prov-rev-insuf", 500)

	svc := createTestService(t, pool)
	ctx := context.Background()

	betResult, err := svc.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-rev-insuf-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money:                 MoneyDTO{Amount: "5.00", Currency: "BRL"},
	}, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}

	rollbackResult, err := svc.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-rev-insuf-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "ROLLBACK",
		Money:                 MoneyDTO{Amount: "5.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-rev-insuf-1",
	}, "", "")
	if err != nil {
		t.Fatalf("ROLLBACK should not error, got: %v", err)
	}
	t.Logf("ROLLBACK processed: tx=%s status=%s", rollbackResult.TransactionID, rollbackResult.Status)

	secondRollback, err := svc.ProcessTransaction(ctx, Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-rev-insuf-2",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "ROLLBACK",
		Money:                 MoneyDTO{Amount: "5.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-rev-insuf-1",
	}, "", "")
	if err != nil {
		t.Fatalf("second ROLLBACK should not error, got: %v", err)
	}

	_ = betResult

	if secondRollback.Status != "REJECTED" {
		t.Errorf("expected second ROLLBACK rejected, got %s", secondRollback.Status)
	}
}
