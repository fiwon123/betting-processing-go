package domain

import (
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

type Event struct {
	EventID       string    `json:"eventId"`
	EventType     string    `json:"eventType"`
	AggregateType string    `json:"aggregateType"`
	AggregateID   string    `json:"aggregateId"`
	CorrelationID string    `json:"correlationId,omitempty"`
	CausationID   string    `json:"causationId,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
	Version       int       `json:"version"`
	Data          any       `json:"data"`
}

type WagerTransactionProcessedData struct {
	TransactionID  string      `json:"transactionId"`
	ExternalID     string      `json:"externalTransactionId,omitempty"`
	ProviderID     string      `json:"providerId"`
	PlayerID       string      `json:"playerId"`
	WalletID       string      `json:"walletId"`
	RoundID        string      `json:"roundId"`
	GameID         string      `json:"gameId"`
	Kind           string      `json:"kind"`
	Money          money.Money `json:"money"`
	ReferenceExtID string      `json:"referenceExternalTransactionId,omitempty"`
	Status         string      `json:"status"`
	FailureCode    string      `json:"failureCode,omitempty"`
}

type WagerTransactionRejectedData struct {
	TransactionID string      `json:"transactionId"`
	ExternalID    string      `json:"externalTransactionId,omitempty"`
	ProviderID    string      `json:"providerId"`
	PlayerID      string      `json:"playerId"`
	WalletID      string      `json:"walletId"`
	Kind          string      `json:"kind"`
	Money         money.Money `json:"money"`
	FailureCode   string      `json:"failureCode"`
}

type WalletBalanceChangedData struct {
	WalletID      string      `json:"walletId"`
	TransactionID string      `json:"transactionId"`
	Direction     string      `json:"direction"`
	Money         money.Money `json:"money"`
	BalanceBefore money.Money `json:"balanceBefore"`
	BalanceAfter  money.Money `json:"balanceAfter"`
	WalletVersion int64       `json:"walletVersion"`
}

type WagerTransactionPendingReferenceData struct {
	TransactionID  string `json:"transactionId"`
	ExternalID     string `json:"externalTransactionId"`
	ProviderID     string `json:"providerId"`
	ReferenceExtID string `json:"referenceExternalTransactionId"`
	PlayerID       string `json:"playerId"`
	WalletID       string `json:"walletId"`
	Kind           string `json:"kind"`
}
