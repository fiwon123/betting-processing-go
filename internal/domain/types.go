package domain

import "encoding/json"

type Direction string

const (
	DEBIT  Direction = "DEBIT"
	CREDIT Direction = "CREDIT"
)

func MarshalEventPayload(data any) ([]byte, error) {
	return json.Marshal(data)
}
