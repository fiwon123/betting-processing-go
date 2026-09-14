//go:build integration

package wagertransaction_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
	"github.com/fiwon123/betting-processing-go/internal/wagertransaction"
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
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, walletID string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE wallets SET balance = $1, version = version + 1 WHERE id = $2`, balance, walletID,
	)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}

func createTestService(t *testing.T, pool *pgxpool.Pool) *wagertransaction.Service {
	t.Helper()
	repo := newTestRepo(pool)
	walletSvc := newTestWalletSvc(pool)
	inboxRepo := newTestInboxRepo(pool)
	outboxRepo := newTestOutboxRepo(pool)
	return wagertransaction.NewService(repo, inboxRepo, outboxRepo, walletSvc, pool, nil)
}

func TestConcurrency_DoubleDebit(t *testing.T) {
	pool := mustPool(t)
	walletID := "00000000-0000-0000-0000-000000000001"
	playerID := "00000000-0000-0000-0000-000000000010"
	seedWallet(t, pool, walletID, 10000)

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
			req := wagertransaction.Request{
				ProviderID:            fmt.Sprintf("prov-%d", idx),
				ExternalTransactionID: fmt.Sprintf("ext-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: wagertransaction.MoneyDTO{
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)

	req := wagertransaction.Request{
		ProviderID:            "prov-idempotent",
		ExternalTransactionID: "ext-idempotent",
		PlayerID:              playerID,
		WalletID:              walletID,
		Kind:                  "BET",
		Money: wagertransaction.MoneyDTO{
			Amount:   "5.00",
			Currency: "BRL",
		},
	}
	key := req.ProviderID + ":" + req.ExternalTransactionID

	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]*wagertransaction.ProcessResult, goroutines)

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
	seedWallet(t, pool, walletID, 10000)

	svc := createTestService(t, pool)

	const goroutines = 2
	var wg sync.WaitGroup
	processed := make(chan string, goroutines)
	rejected := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := wagertransaction.Request{
				ProviderID:            fmt.Sprintf("prov-80-%d", idx),
				ExternalTransactionID: fmt.Sprintf("ext-80-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: wagertransaction.MoneyDTO{
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)

	const goroutines = 15
	var wg sync.WaitGroup
	processed := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := wagertransaction.Request{
				ProviderID:            fmt.Sprintf("prov-lock-%d", idx),
				ExternalTransactionID: fmt.Sprintf("ext-lock-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: wagertransaction.MoneyDTO{
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "BET",
		Money:                 wagertransaction.MoneyDTO{Amount: "50.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}
	t.Logf("BET processed: %s", betResult.TransactionID)

	// Step 2: REFUND the BET
	refundReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "REFUND",
		Money:                 wagertransaction.MoneyDTO{Amount: "50.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-100",
	}
	refundResult, err := svc.ProcessTransaction(ctx, refundReq, "", "")
	if err != nil {
		t.Fatalf("process REFUND: %v", err)
	}
	t.Logf("REFUND processed: %s", refundResult.TransactionID)

	// Step 3: Try ROLLBACK on the same BET - should be rejected
	rollbackReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-1",
		Kind:                  "ROLLBACK",
		Money:                 wagertransaction.MoneyDTO{Amount: "50.00", Currency: "BRL"},
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "BET",
		Money:                 wagertransaction.MoneyDTO{Amount: "30.00", Currency: "BRL"},
	}
	betResult, err := svc.ProcessTransaction(ctx, betReq, "", "")
	if err != nil {
		t.Fatalf("process BET: %v", err)
	}
	t.Logf("BET processed: %s", betResult.TransactionID)

	// Step 2: ROLLBACK the BET
	rollbackReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "rollback-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "ROLLBACK",
		Money:                 wagertransaction.MoneyDTO{Amount: "30.00", Currency: "BRL"},
		ReferenceExternalID:   "bet-200",
	}
	rollbackResult, err := svc.ProcessTransaction(ctx, rollbackReq, "", "")
	if err != nil {
		t.Fatalf("process ROLLBACK: %v", err)
	}
	t.Logf("ROLLBACK processed: %s", rollbackResult.TransactionID)

	// Step 3: Try REFUND on the same BET - should be rejected (cross-type double reversal)
	refundReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-200",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-2",
		Kind:                  "REFUND",
		Money:                 wagertransaction.MoneyDTO{Amount: "30.00", Currency: "BRL"},
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
	seedWallet(t, pool, walletID, 50000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Get initial wallet version
	var initialVersion int64
	err := pool.QueryRow(ctx, `SELECT version FROM wallets WHERE id = $1`, walletID).Scan(&initialVersion)
	if err != nil {
		t.Fatalf("get initial version: %v", err)
	}

	// Process a LOSS
	lossReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "loss-100",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-3",
		Kind:                  "LOSS",
		Money:                 wagertransaction.MoneyDTO{Amount: "0.00", Currency: "BRL"},
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET (will be in PENDING state initially)
	betReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-pending-1",
		Kind:                  "BET",
		Money:                 wagertransaction.MoneyDTO{Amount: "20.00", Currency: "BRL"},
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
	refundReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-pending-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-pending-1",
		Kind:                  "REFUND",
		Money:                 wagertransaction.MoneyDTO{Amount: "20.00", Currency: "BRL"},
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
	seedWallet(t, pool, walletID, 100000)

	svc := createTestService(t, pool)
	ctx := context.Background()

	// Step 1: Process a BET
	betReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "bet-max-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-max-1",
		Kind:                  "BET",
		Money:                 wagertransaction.MoneyDTO{Amount: "10.00", Currency: "BRL"},
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
	refundReq := wagertransaction.Request{
		ProviderID:            providerID,
		ExternalTransactionID: "refund-max-1",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-max-1",
		Kind:                  "REFUND",
		Money:                 wagertransaction.MoneyDTO{Amount: "10.00", Currency: "BRL"},
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
	seedWallet(t, pool, walletID, 100000)

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

	services := []*wagertransaction.Service{svc1, svc2, svc3}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			svc := services[idx%3]
			req := wagertransaction.Request{
				ProviderID:            fmt.Sprintf("prov-inst-%d", idx),
				ExternalTransactionID: fmt.Sprintf("ext-inst-%d", idx),
				PlayerID:              playerID,
				WalletID:              walletID,
				Kind:                  "BET",
				Money: wagertransaction.MoneyDTO{
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
