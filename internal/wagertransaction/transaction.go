package wagertransaction

import (
	"time"

	"github.com/fiwon123/betting-processing-go/internal/money"
)

type TransactionType string

const (
	OPENING  TransactionType = "OPENING"
	BET      TransactionType = "BET"
	WIN      TransactionType = "WIN"
	LOSS     TransactionType = "LOSS"
	REFUND   TransactionType = "REFUND"
	ROLLBACK TransactionType = "ROLLBACK"
)

type TransactionStatus string

const (
	PENDING           TransactionStatus = "PENDING"
	PENDING_REFERENCE TransactionStatus = "PENDING_REFERENCE"
	PROCESSED         TransactionStatus = "PROCESSED"
	REJECTED          TransactionStatus = "REJECTED"
	FAILED            TransactionStatus = "FAILED"
)

type Origin string

const (
	INTERNAL Origin = "INTERNAL"
	EXTERNAL Origin = "EXTERNAL"
)

type Transaction struct {
	id                string
	origin            Origin
	externalID        string
	provider          string
	idempotencyKey    string
	payloadHash       string
	walletID          string
	playerID          string
	roundID           string
	gameID            string
	transactionType   TransactionType
	amount            money.Money
	externalReference string
	internalReference string
	status            TransactionStatus
	failureCode       string
	refAttempts       int
	refNextAttemptAt  *time.Time
	resultBalance     *money.Money
	createdAt         time.Time
	updatedAt         time.Time
	processedAt       *time.Time
}

func NewExternalTransaction(
	id, provider, externalID, idempotencyKey, payloadHash,
	walletID, playerID, roundID, gameID string,
	transactionType TransactionType,
	amount money.Money,
	externalReference string,
) (*Transaction, error) {
	if transactionType == OPENING {
		return nil, ErrOPENINGRejected
	}
	if provider == "" {
		return nil, ErrInvalidProvider
	}
	if externalID == "" {
		return nil, ErrInvalidExternalID
	}
	if idempotencyKey == "" {
		return nil, ErrInvalidIdempotencyKey
	}
	if walletID == "" {
		return nil, ErrInvalidWalletID
	}
	if playerID == "" {
		return nil, ErrInvalidPlayerID
	}
	if !transactionType.Valid() {
		return nil, ErrInvalidTransactionType
	}
	if transactionType != LOSS && (amount.IsNegative() || amount.IsZero()) {
		return nil, ErrPositiveAmountRequired
	}
	if transactionType == LOSS && !amount.IsZero() {
		return nil, ErrZeroAmountRequired
	}

	now := time.Now().UTC()
	return &Transaction{
		id:                id,
		origin:            EXTERNAL,
		externalID:        externalID,
		provider:          provider,
		idempotencyKey:    idempotencyKey,
		payloadHash:       payloadHash,
		walletID:          walletID,
		playerID:          playerID,
		roundID:           roundID,
		gameID:            gameID,
		transactionType:   transactionType,
		amount:            amount,
		externalReference: externalReference,
		status:            PENDING,
		createdAt:         now,
		updatedAt:         now,
	}, nil
}

func NewOpeningTransaction(
	id, walletID, playerID string,
	amount money.Money,
) (*Transaction, error) {
	if id == "" {
		return nil, ErrInvalidTransactionID
	}
	if walletID == "" {
		return nil, ErrInvalidWalletID
	}
	if playerID == "" {
		return nil, ErrInvalidPlayerID
	}
	if amount.IsNegative() {
		return nil, ErrInvalidMoney
	}

	now := time.Now().UTC()
	return &Transaction{
		id:              id,
		origin:          INTERNAL,
		walletID:        walletID,
		playerID:        playerID,
		transactionType: OPENING,
		amount:          amount,
		status:          PROCESSED,
		createdAt:       now,
		updatedAt:       now,
		processedAt:     &now,
	}, nil
}

func RehydrateTransaction(
	id string,
	origin Origin,
	externalID, provider, idempotencyKey, payloadHash string,
	walletID, playerID, roundID, gameID string,
	transactionType TransactionType,
	amount money.Money,
	externalReference, internalReference string,
	status TransactionStatus,
	failureCode string,
	refAttempts int,
	refNextAttemptAt *time.Time,
	createdAt, updatedAt time.Time,
	processedAt *time.Time,
) *Transaction {
	return &Transaction{
		id:                id,
		origin:            origin,
		externalID:        externalID,
		provider:          provider,
		idempotencyKey:    idempotencyKey,
		payloadHash:       payloadHash,
		walletID:          walletID,
		playerID:          playerID,
		roundID:           roundID,
		gameID:            gameID,
		transactionType:   transactionType,
		amount:            amount,
		externalReference: externalReference,
		internalReference: internalReference,
		status:            status,
		failureCode:       failureCode,
		refAttempts:       refAttempts,
		refNextAttemptAt:  refNextAttemptAt,
		createdAt:         createdAt,
		updatedAt:         updatedAt,
		processedAt:       processedAt,
	}
}

func (t *Transaction) ID() string                       { return t.id }
func (t *Transaction) Origin() Origin                   { return t.origin }
func (t *Transaction) ExternalID() string               { return t.externalID }
func (t *Transaction) Provider() string                 { return t.provider }
func (t *Transaction) IdempotencyKey() string           { return t.idempotencyKey }
func (t *Transaction) PayloadHash() string              { return t.payloadHash }
func (t *Transaction) WalletID() string                 { return t.walletID }
func (t *Transaction) PlayerID() string                 { return t.playerID }
func (t *Transaction) RoundID() string                  { return t.roundID }
func (t *Transaction) GameID() string                   { return t.gameID }
func (t *Transaction) TransactionType() TransactionType { return t.transactionType }
func (t *Transaction) Amount() money.Money              { return t.amount }
func (t *Transaction) ExternalReference() string        { return t.externalReference }
func (t *Transaction) InternalReference() string        { return t.internalReference }
func (t *Transaction) Status() TransactionStatus        { return t.status }
func (t *Transaction) FailureCode() string              { return t.failureCode }
func (t *Transaction) CreatedAt() time.Time             { return t.createdAt }
func (t *Transaction) UpdatedAt() time.Time             { return t.updatedAt }
func (t *Transaction) ProcessedAt() *time.Time          { return t.processedAt }
func (t *Transaction) IsTerminal() bool {
	return t.status == PROCESSED || t.status == REJECTED || t.status == FAILED
}

func (t *Transaction) TransitionTo(newStatus TransactionStatus) error {
	if t.IsTerminal() {
		return ErrInvalidStateTransition
	}
	if !isValidTransition(t.status, newStatus) {
		return ErrInvalidStateTransition
	}
	t.status = newStatus
	t.updatedAt = time.Now().UTC()
	if newStatus == PROCESSED {
		now := time.Now().UTC()
		t.processedAt = &now
	}
	return nil
}

func (t *Transaction) SetFailureCode(code string) {
	t.failureCode = code
	t.updatedAt = time.Now().UTC()
}

func (t *Transaction) SetInternalReference(ref string) {
	t.internalReference = ref
	t.updatedAt = time.Now().UTC()
}

func (t *Transaction) SetID(id string) {
	t.id = id
}

func (t *Transaction) RefAttempts() int             { return t.refAttempts }
func (t *Transaction) RefNextAttemptAt() *time.Time { return t.refNextAttemptAt }

func (t *Transaction) IncrementRefAttempts() {
	t.refAttempts++
	now := time.Now().UTC()
	backoff := 1 << min(t.refAttempts, 6)
	next := now.Add(time.Duration(backoff) * time.Second)
	t.refNextAttemptAt = &next
	t.updatedAt = now
}

func (t *Transaction) SetRefNextAttemptAt(at *time.Time) {
	t.refNextAttemptAt = at
}

func (t *Transaction) ResultBalance() *money.Money { return t.resultBalance }

func (t *Transaction) SetResultBalance(b money.Money) {
	t.resultBalance = &b
}

func (tt TransactionType) Valid() bool {
	switch tt {
	case BET, WIN, LOSS, REFUND, ROLLBACK:
		return true
	default:
		return false
	}
}

func isValidTransition(from, to TransactionStatus) bool {
	switch from {
	case PENDING:
		return to == PROCESSED || to == REJECTED || to == FAILED || to == PENDING_REFERENCE
	case PENDING_REFERENCE:
		return to == PROCESSED || to == REJECTED || to == FAILED
	default:
		return false
	}
}
