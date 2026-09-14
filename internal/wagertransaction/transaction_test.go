package wagertransaction

import (
	"testing"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

func TestNewExternalBET(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, err := NewExternalTransaction(
		"tx1", "provider-a", "ext-1", "provider-a:ext-1", "hash1",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, amount, "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.TransactionType() != BET {
		t.Fatalf("expected BET, got %s", tx.TransactionType())
	}
	if tx.Status() != PENDING {
		t.Fatalf("expected PENDING, got %s", tx.Status())
	}
	if tx.Origin() != EXTERNAL {
		t.Fatalf("expected EXTERNAL, got %s", tx.Origin())
	}
}

func TestNewExternalWIN(t *testing.T) {
	amount, _ := money.NewMoney(5000, money.BRL)
	tx, err := NewExternalTransaction(
		"tx2", "provider-a", "ext-2", "provider-a:ext-2", "hash2",
		"wallet-1", "player-1", "round-1", "game-1",
		WIN, amount, "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.TransactionType() != WIN {
		t.Fatalf("expected WIN, got %s", tx.TransactionType())
	}
}

func TestNewExternalLOSSRequiresZeroAmount(t *testing.T) {
	nonZero, _ := money.NewMoney(100, money.BRL)
	_, err := NewExternalTransaction(
		"tx3", "provider-a", "ext-3", "provider-a:ext-3", "hash3",
		"wallet-1", "player-1", "round-1", "game-1",
		LOSS, nonZero, "",
	)
	if err != ErrZeroAmountRequired {
		t.Fatalf("expected ErrZeroAmountRequired, got %v", err)
	}
}

func TestNewExternalLOSSAcceptsZeroAmount(t *testing.T) {
	zero, _ := money.NewMoney(0, money.BRL)
	tx, err := NewExternalTransaction(
		"tx4", "provider-a", "ext-4", "provider-a:ext-4", "hash4",
		"wallet-1", "player-1", "round-1", "game-1",
		LOSS, zero, "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.TransactionType() != LOSS {
		t.Fatalf("expected LOSS, got %s", tx.TransactionType())
	}
}

func TestNewExternalREJECTS_OPENING(t *testing.T) {
	amount, _ := money.NewMoney(1000, money.BRL)
	_, err := NewExternalTransaction(
		"tx5", "provider-a", "ext-5", "provider-a:ext-5", "hash5",
		"wallet-1", "player-1", "round-1", "game-1",
		OPENING, amount, "",
	)
	if err != ErrOPENINGRejected {
		t.Fatalf("expected ErrOPENINGRejected, got %v", err)
	}
}

func TestNewExternalREJECTSNegativeAmount(t *testing.T) {
	neg, _ := money.NewMoney(-1000, money.BRL) // -10.00
	_, err := NewExternalTransaction(
		"tx6", "provider-a", "ext-6", "provider-a:ext-6", "hash6",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, neg, "",
	)
	if err != ErrPositiveAmountRequired {
		t.Fatalf("expected ErrPositiveAmountRequired, got %v", err)
	}
}

func TestNewExternalREJECTSZeroAmountForBET(t *testing.T) {
	zero, _ := money.NewMoney(0, money.BRL)
	_, err := NewExternalTransaction(
		"tx7", "provider-a", "ext-7", "provider-a:ext-7", "hash7",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, zero, "",
	)
	if err != ErrPositiveAmountRequired {
		t.Fatalf("expected ErrPositiveAmountRequired, got %v", err)
	}
}

func TestNewOpeningTransaction(t *testing.T) {
	amount, _ := money.NewMoney(10000, money.BRL)
	tx, err := NewOpeningTransaction("op1", "wallet-1", "player-1", amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.TransactionType() != OPENING {
		t.Fatalf("expected OPENING, got %s", tx.TransactionType())
	}
	if tx.Origin() != INTERNAL {
		t.Fatalf("expected INTERNAL, got %s", tx.Origin())
	}
	if tx.Status() != PROCESSED {
		t.Fatalf("expected PROCESSED, got %s", tx.Status())
	}
	if tx.ProcessedAt() == nil {
		t.Fatal("expected processedAt to be set")
	}
}

func TestTransitionToProcessed(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, _ := NewExternalTransaction(
		"tx8", "provider-a", "ext-8", "provider-a:ext-8", "hash8",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, amount, "",
	)

	err := tx.TransitionTo(PROCESSED)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != PROCESSED {
		t.Fatalf("expected PROCESSED, got %s", tx.Status())
	}
	if !tx.IsTerminal() {
		t.Fatal("expected terminal state")
	}
}

func TestTransitionToRejected(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, _ := NewExternalTransaction(
		"tx9", "provider-a", "ext-9", "provider-a:ext-9", "hash9",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, amount, "",
	)

	err := tx.TransitionTo(REJECTED)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != REJECTED {
		t.Fatalf("expected REJECTED, got %s", tx.Status())
	}
}

func TestTransitionToPendingReference(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, _ := NewExternalTransaction(
		"tx10", "provider-a", "ext-10", "provider-a:ext-10", "hash10",
		"wallet-1", "player-1", "round-1", "game-1",
		REFUND, amount, "ref-ext-1",
	)

	err := tx.TransitionTo(PENDING_REFERENCE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status() != PENDING_REFERENCE {
		t.Fatalf("expected PENDING_REFERENCE, got %s", tx.Status())
	}
	if tx.IsTerminal() {
		t.Fatal("PENDING_REFERENCE should not be terminal")
	}
}

func TestTransitionFromTerminalRejected(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, _ := NewExternalTransaction(
		"tx11", "provider-a", "ext-11", "provider-a:ext-11", "hash11",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, amount, "",
	)
	tx.TransitionTo(PROCESSED)

	err := tx.TransitionTo(REJECTED)
	if err != ErrInvalidStateTransition {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
}

func TestInvalidTransitionPendingToRejected(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, _ := NewExternalTransaction(
		"tx12", "provider-a", "ext-12", "provider-a:ext-12", "hash12",
		"wallet-1", "player-1", "round-1", "game-1",
		BET, amount, "",
	)
	tx.TransitionTo(PENDING_REFERENCE)

	// From PENDING_REFERENCE, going to PENDING is not valid
	err := tx.TransitionTo(PENDING)
	if err != ErrInvalidStateTransition {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
}

func TestREFUNDWithReference(t *testing.T) {
	amount, _ := money.NewMoney(2500, money.BRL)
	tx, err := NewExternalTransaction(
		"tx13", "provider-a", "ext-13", "provider-a:ext-13", "hash13",
		"wallet-1", "player-1", "round-1", "game-1",
		REFUND, amount, "original-ext-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.ExternalReference() != "original-ext-1" {
		t.Fatalf("expected reference original-ext-1, got %s", tx.ExternalReference())
	}
}

func TestTransactionTypeValidation(t *testing.T) {
	validTypes := []TransactionType{BET, WIN, LOSS, REFUND, ROLLBACK}
	for _, tt := range validTypes {
		if !tt.Valid() {
			t.Fatalf("expected %s to be valid", tt)
		}
	}
	if OPENING.Valid() {
		t.Fatal("expected OPENING to be invalid for external")
	}
	if TransactionType("INVALID").Valid() {
		t.Fatal("expected INVALID to be invalid")
	}
}
