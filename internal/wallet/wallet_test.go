package wallet

import (
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

func mustMoney(t *testing.T, amount int64, currency money.Currency) money.Money {
	t.Helper()

	result, err := money.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("failed to create money: %v", err)
	}

	return result
}

func TestNewWallet(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)

	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.ID() != "w1" {
		t.Fatalf("expected ID w1, got %s", w.ID())
	}

	if w.PlayerID() != "p1" {
		t.Fatalf("expected playerID p1, got %s", w.PlayerID())
	}

	if w.ProviderID() != "provider-a" {
		t.Fatalf("expected providerID provider-a, got %s", w.ProviderID())
	}

	if w.Currency() != money.BRL {
		t.Fatalf("expected BRL currency, got %s", w.Currency())
	}

	if w.Version() != 1 {
		t.Fatalf("expected version 1, got %d", w.Version())
	}

	cmp, err := w.Balance().Compare(bal)
	if err != nil {
		t.Fatalf("compare balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf("expected balance 100.00, got %s", w.Balance().String())
	}

	if w.CreatedAt().IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	if w.UpdatedAt().IsZero() {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestNewWalletTrimsIdentifiers(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)

	w, err := NewWallet(
		"  w1  ",
		"  p1  ",
		"  provider-a  ",
		bal,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.ID() != "w1" {
		t.Fatalf("expected trimmed ID, got %q", w.ID())
	}

	if w.PlayerID() != "p1" {
		t.Fatalf("expected trimmed player ID, got %q", w.PlayerID())
	}

	if w.ProviderID() != "provider-a" {
		t.Fatalf("expected trimmed provider ID, got %q", w.ProviderID())
	}
}

func TestNewWalletRejectsEmptyID(t *testing.T) {
	bal := mustMoney(t, 0, money.BRL)

	_, err := NewWallet("", "p1", "provider-a", bal)
	if err != ErrInvalidWalletID {
		t.Fatalf("expected ErrInvalidWalletID, got %v", err)
	}
}

func TestNewWalletRejectsEmptyPlayerID(t *testing.T) {
	bal := mustMoney(t, 0, money.BRL)

	_, err := NewWallet("w1", "", "provider-a", bal)
	if err != ErrInvalidPlayerID {
		t.Fatalf("expected ErrInvalidPlayerID, got %v", err)
	}
}

func TestNewWalletRejectsEmptyProviderID(t *testing.T) {
	bal := mustMoney(t, 0, money.BRL)

	_, err := NewWallet("w1", "p1", "", bal)
	if err != ErrInvalidProviderID {
		t.Fatalf("expected ErrInvalidProviderID, got %v", err)
	}
}

func TestNewWalletRejectsInvalidCurrency(t *testing.T) {
	bal := mustMoney(t, 10000, money.Currency("USD"))

	_, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != ErrInvalidCurrency {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestNewWalletRejectsNegativeBalance(t *testing.T) {
	bal := mustMoney(t, -1000, money.BRL)

	_, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != ErrNegativeBalance {
		t.Fatalf("expected ErrNegativeBalance, got %v", err)
	}
}

func TestRehydrateWallet(t *testing.T) {
	createdAt := time.Date(
		2026,
		time.January,
		1,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	updatedAt := createdAt.Add(time.Hour)
	bal := mustMoney(t, 10000, money.BRL)

	w, err := RehydrateWallet(
		"w1",
		"p1",
		"provider-a",
		money.BRL,
		bal,
		5,
		createdAt,
		updatedAt,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.ID() != "w1" {
		t.Fatalf("expected ID w1, got %s", w.ID())
	}

	if w.Version() != 5 {
		t.Fatalf("expected version 5, got %d", w.Version())
	}

	if !w.CreatedAt().Equal(createdAt) {
		t.Fatalf("unexpected CreatedAt: %v", w.CreatedAt())
	}

	if !w.UpdatedAt().Equal(updatedAt) {
		t.Fatalf("unexpected UpdatedAt: %v", w.UpdatedAt())
	}
}

func TestRehydrateWalletRejectsInvalidVersion(t *testing.T) {
	now := time.Now().UTC()
	bal := mustMoney(t, 10000, money.BRL)

	_, err := RehydrateWallet(
		"w1",
		"p1",
		"provider-a",
		money.BRL,
		bal,
		0,
		now,
		now,
	)
	if err != ErrInvalidWalletVersion {
		t.Fatalf("expected ErrInvalidWalletVersion, got %v", err)
	}
}

func TestRehydrateWalletRejectsInvalidTimestamp(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)
	now := time.Now().UTC()

	_, err := RehydrateWallet(
		"w1",
		"p1",
		"provider-a",
		money.BRL,
		bal,
		1,
		time.Time{},
		now,
	)
	if err != ErrInvalidWalletTimestamp {
		t.Fatalf("expected ErrInvalidWalletTimestamp, got %v", err)
	}
}

func TestRehydrateWalletRejectsUpdatedAtBeforeCreatedAt(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)
	now := time.Now().UTC()

	_, err := RehydrateWallet(
		"w1",
		"p1",
		"provider-a",
		money.BRL,
		bal,
		1,
		now,
		now.Add(-time.Second),
	)
	if err != ErrInvalidWalletTimestamp {
		t.Fatalf("expected ErrInvalidWalletTimestamp, got %v", err)
	}
}

func TestWalletDebit(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 2500, money.BRL)

	change, err := w.Debit(amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedAfter := mustMoney(t, 7500, money.BRL)

	cmp, err := change.BalanceAfter.Compare(expectedAfter)
	if err != nil {
		t.Fatalf("compare balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf(
			"expected balance after 75.00, got %s",
			change.BalanceAfter.String(),
		)
	}

	if change.NewVersion != 2 {
		t.Fatalf("expected new version 2, got %d", change.NewVersion)
	}

	if w.Version() != 2 {
		t.Fatalf("expected wallet version 2, got %d", w.Version())
	}
}

func TestWalletDebitInsufficientBalance(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 2000, money.BRL)

	_, err = w.Debit(amount)
	if err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}

	if w.Version() != 1 {
		t.Fatalf("wallet changed after failed debit")
	}
}

func TestWalletDebitExactBalance(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 1000, money.BRL)

	change, err := w.Debit(amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	zero := mustMoney(t, 0, money.BRL)

	cmp, err := change.BalanceAfter.Compare(zero)
	if err != nil {
		t.Fatalf("compare balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf(
			"expected balance after 0.00, got %s",
			change.BalanceAfter.String(),
		)
	}
}

func TestWalletDebitRejectsZero(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	zero := mustMoney(t, 0, money.BRL)

	_, err = w.Debit(zero)
	if err != ErrPositiveAmountRequired {
		t.Fatalf("expected ErrPositiveAmountRequired, got %v", err)
	}
}

func TestWalletDebitRejectsCurrencyMismatch(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 100, money.Currency("USD"))

	_, err = w.Debit(amount)
	if err != ErrCurrencyMismatch {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestWalletCredit(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 500, money.BRL)

	change, err := w.Credit(amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedAfter := mustMoney(t, 1500, money.BRL)

	cmp, err := change.BalanceAfter.Compare(expectedAfter)
	if err != nil {
		t.Fatalf("compare balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf(
			"expected balance after 15.00, got %s",
			change.BalanceAfter.String(),
		)
	}

	if change.NewVersion != 2 {
		t.Fatalf("expected new version 2, got %d", change.NewVersion)
	}
}

func TestWalletCreditRejectsZero(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	zero := mustMoney(t, 0, money.BRL)

	_, err = w.Credit(zero)
	if err != ErrPositiveAmountRequired {
		t.Fatalf("expected ErrPositiveAmountRequired, got %v", err)
	}
}

func TestWalletCreditRejectsNegativeAmount(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, -100, money.BRL)

	_, err = w.Credit(amount)
	if err != ErrPositiveAmountRequired {
		t.Fatalf("expected ErrPositiveAmountRequired, got %v", err)
	}
}

func TestWalletCanDebit(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 500, money.BRL)

	if !w.CanDebit(amount) {
		t.Fatal("expected CanDebit to return true")
	}

	tooMuch := mustMoney(t, 2000, money.BRL)

	if w.CanDebit(tooMuch) {
		t.Fatal("expected CanDebit to return false for excessive amount")
	}
}

func TestWalletCanDebitRejectsCurrencyMismatch(t *testing.T) {
	bal := mustMoney(t, 1000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 100, money.Currency("USD"))

	if w.CanDebit(amount) {
		t.Fatal("expected CanDebit to return false for currency mismatch")
	}
}

func TestLedgerEntryCreation(t *testing.T) {
	balBefore := mustMoney(t, 10000, money.BRL)
	balAfter := mustMoney(t, 7500, money.BRL)
	amount := mustMoney(t, 2500, money.BRL)

	entry, err := NewLedgerEntry(
		"e1",
		"w1",
		"t1",
		DEBIT,
		amount,
		balBefore,
		balAfter,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.ID() != "e1" {
		t.Fatalf("expected ID e1, got %s", entry.ID())
	}

	if entry.Direction() != DEBIT {
		t.Fatalf("expected DEBIT, got %s", entry.Direction())
	}
}

func TestLedgerEntryCreationAllowsDatabaseGeneratedID(t *testing.T) {
	balBefore := mustMoney(t, 0, money.BRL)
	balAfter := mustMoney(t, 1000, money.BRL)
	amount := mustMoney(t, 1000, money.BRL)

	_, err := NewLedgerEntry(
		"",
		"w1",
		"t1",
		CREDIT,
		amount,
		balBefore,
		balAfter,
	)
	if err != nil {
		t.Fatalf("expected empty ID to be allowed, got %v", err)
	}
}

func TestLedgerEntryRejectsEmptyFields(t *testing.T) {
	balBefore := mustMoney(t, 10000, money.BRL)
	balAfter := mustMoney(t, 7500, money.BRL)
	amount := mustMoney(t, 2500, money.BRL)

	_, err := NewLedgerEntry(
		"e1",
		"",
		"t1",
		DEBIT,
		amount,
		balBefore,
		balAfter,
	)
	if err != ErrInvalidLedgerEntry {
		t.Fatalf("expected ErrInvalidLedgerEntry for empty walletID, got %v", err)
	}

	_, err = NewLedgerEntry(
		"e1",
		"w1",
		"",
		DEBIT,
		amount,
		balBefore,
		balAfter,
	)
	if err != ErrInvalidLedgerEntry {
		t.Fatalf(
			"expected ErrInvalidLedgerEntry for empty transactionID, got %v",
			err,
		)
	}
}

func TestLedgerEntryRejectsInvalidMath(t *testing.T) {
	balBefore := mustMoney(t, 10000, money.BRL)
	wrongAfter := mustMoney(t, 8000, money.BRL)
	amount := mustMoney(t, 2500, money.BRL)

	_, err := NewLedgerEntry(
		"e1",
		"w1",
		"t1",
		DEBIT,
		amount,
		balBefore,
		wrongAfter,
	)
	if err != ErrLedgerMathInvalid {
		t.Fatalf("expected ErrLedgerMathInvalid, got %v", err)
	}
}

func TestLedgerEntryRejectsCurrencyMismatch(t *testing.T) {
	balBefore := mustMoney(t, 10000, money.BRL)
	balAfter := mustMoney(t, 7500, money.BRL)
	amount := mustMoney(t, 2500, money.Currency("USD"))

	_, err := NewLedgerEntry(
		"e1",
		"w1",
		"t1",
		DEBIT,
		amount,
		balBefore,
		balAfter,
	)
	if err != ErrCurrencyMismatch {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestConcurrentDebitOnSameWallet(t *testing.T) {
	bal := mustMoney(t, 10000, money.BRL)
	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	amount := mustMoney(t, 10000, money.BRL)

	const workers = 20

	var wg sync.WaitGroup
	var mu sync.Mutex

	successes := 0
	insufficientBalanceErrors := 0

	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()

			_, err := w.Debit(amount)

			mu.Lock()
			defer mu.Unlock()

			switch err {
			case nil:
				successes++
			case ErrInsufficientBalance:
				insufficientBalanceErrors++
			default:
				t.Errorf("unexpected debit error: %v", err)
			}
		}()
	}

	wg.Wait()

	if successes != 1 {
		t.Fatalf("expected exactly one successful debit, got %d", successes)
	}

	if insufficientBalanceErrors != workers-1 {
		t.Fatalf(
			"expected %d insufficient balance errors, got %d",
			workers-1,
			insufficientBalanceErrors,
		)
	}

	if w.Version() != 2 {
		t.Fatalf("expected wallet version 2, got %d", w.Version())
	}

	zero := mustMoney(t, 0, money.BRL)

	cmp, err := w.Balance().Compare(zero)
	if err != nil {
		t.Fatalf("compare final balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf("expected final balance 0, got %s", w.Balance().String())
	}
}

func TestMoneyParsingInWallet(t *testing.T) {
	bal := money.MustParse("100.00", money.BRL)

	w, err := NewWallet("w1", "p1", "provider-a", bal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := mustMoney(t, 10000, money.BRL)

	cmp, err := w.Balance().Compare(expected)
	if err != nil {
		t.Fatalf("compare balance: %v", err)
	}

	if cmp != 0 {
		t.Fatalf("expected 100.00, got %s", w.Balance().String())
	}
}
