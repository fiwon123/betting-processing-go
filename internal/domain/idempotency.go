package domain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

func ComputePayloadHash(providerID, externalTransactionID, playerID, walletID, roundID, gameID, kind, amount, currency, referenceExtID string) string {
	fields := map[string]string{
		"providerId": providerID,
		"externalId": externalTransactionID,
		"playerId":   playerID,
		"walletId":   walletID,
		"roundId":    roundID,
		"gameId":     gameID,
		"kind":       kind,
		"amount":     amount,
		"currency":   currency,
	}
	if referenceExtID != "" {
		fields["referenceExternalTransactionId"] = referenceExtID
	}

	sortedKeys := make([]string, 0, len(fields))
	for k := range fields {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	ordered := make(map[string]string, len(fields))
	for _, k := range sortedKeys {
		ordered[k] = fields[k]
	}

	data, _ := json.Marshal(ordered)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)
}
