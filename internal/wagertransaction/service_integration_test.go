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
