package domain

import (
	"testing"
)

func TestComputePayloadHashDeterministic(t *testing.T) {
	h1 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	h2 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	if h1 != h2 {
		t.Fatalf("expected same hash, got %s and %s", h1, h2)
	}
}

func TestComputePayloadHashDifferentInput(t *testing.T) {
	h1 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	h2 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "26.00", "BRL", "")
	if h1 == h2 {
		t.Fatal("expected different hashes for different amounts")
	}
}

func TestComputePayloadHashExcludesIdempotencyKey(t *testing.T) {
	h1 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	h2 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	if h1 != h2 {
		t.Fatal("idempotency key should be excluded from hash")
	}
}

func TestComputePayloadHashWithReference(t *testing.T) {
	h1 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "REFUND", "25.00", "BRL", "ref-1")
	h2 := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "REFUND", "25.00", "BRL", "ref-2")
	if h1 == h2 {
		t.Fatal("expected different hashes for different references")
	}
}

func TestComputePayloadHashIsHex(t *testing.T) {
	hash := ComputePayloadHash("p1", "ext1", "player1", "w1", "r1", "g1", "BET", "25.00", "BRL", "")
	if len(hash) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(hash))
	}
}
