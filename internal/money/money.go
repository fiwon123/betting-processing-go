package money

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Money struct {
	amount   int64
	currency Currency
}

func NewMoney(amount int64, currency Currency) (Money, error) {
	if !currency.Valid() {
		return Money{}, ErrUnsupportedCurrency
	}

	return Money{
		amount:   amount,
		currency: currency,
	}, nil
}

func ParseMoney(input string, currency Currency) (Money, error) {
	if !currency.Valid() {
		return Money{}, ErrUnsupportedCurrency
	}

	if input == "" {
		return Money{}, ErrInvalidMoney
	}

	if strings.TrimSpace(input) != input {
		return Money{}, ErrInvalidMoney
	}

	if input == "NaN" ||
		input == "Inf" ||
		input == "Infinity" ||
		input == "INF" ||
		input == "INFINITY" ||
		strings.ContainsAny(input, "eE+") {
		return Money{}, ErrScientificNotation
	}

	if strings.ContainsAny(input, ", ") ||
		strings.ContainsAny(input, "\t\r\n") {
		return Money{}, ErrInvalidMoney
	}

	if strings.HasPrefix(input, "-") {
		return Money{}, ErrNegativeExternal
	}

	parts := strings.Split(input, ".")
	if len(parts) > 2 {
		return Money{}, ErrInvalidMoney
	}

	wholePart := parts[0]
	if !isDigits(wholePart) {
		return Money{}, ErrInvalidMoney
	}

	whole, err := strconv.ParseUint(wholePart, 10, 64)
	if err != nil {
		return Money{}, ErrOverflow
	}

	minor := uint64(0)

	if len(parts) == 2 {
		minorPart := parts[1]

		if minorPart == "" {
			return Money{}, ErrInvalidMoney
		}

		if len(minorPart) > 2 {
			return Money{}, ErrScaleExceeded
		}

		if !isDigits(minorPart) {
			return Money{}, ErrInvalidMoney
		}

		if len(minorPart) == 1 {
			minorPart += "0"
		}

		minor, err = strconv.ParseUint(minorPart, 10, 64)
		if err != nil {
			return Money{}, ErrInvalidMoney
		}
	}

	maxWhole := uint64(math.MaxInt64) / 100
	maxMinor := uint64(math.MaxInt64) % 100

	if whole > maxWhole ||
		(whole == maxWhole && minor > maxMinor) {
		return Money{}, ErrOverflow
	}

	return NewMoney(int64(whole*100+minor), currency)
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}

	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func MustParse(input string, currency Currency) Money {
	money, err := ParseMoney(input, currency)
	if err != nil {
		panic(fmt.Sprintf(
			"money.MustParse(%q, %s): %v",
			input,
			currency,
			err,
		))
	}

	return money
}

func ZeroMoney(currency Currency) (Money, error) {
	return NewMoney(0, currency)
}

func (m Money) Amount() int64 {
	return m.amount
}

func (m Money) Currency() Currency {
	return m.currency
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	if other.amount > 0 &&
		m.amount > math.MaxInt64-other.amount {
		return Money{}, ErrOverflow
	}

	if other.amount < 0 &&
		m.amount < math.MinInt64-other.amount {
		return Money{}, ErrOverflow
	}

	return Money{
		amount:   m.amount + other.amount,
		currency: m.currency,
	}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	if other.amount == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return m.Add(Money{
		amount:   -other.amount,
		currency: other.currency,
	})
}

func (m Money) Negate() (Money, error) {
	if m.amount == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return Money{
		amount:   -m.amount,
		currency: m.currency,
	}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if err := m.checkCurrency(other); err != nil {
		return 0, err
	}

	switch {
	case m.amount < other.amount:
		return -1, nil
	case m.amount > other.amount:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) IsZero() bool {
	return m.amount == 0
}

func (m Money) IsNegative() bool {
	return m.amount < 0
}

func (m Money) String() string {
	if m.amount < 0 {
		magnitude := uint64(-(m.amount + 1)) + 1
		whole := magnitude / 100
		minor := magnitude % 100

		return fmt.Sprintf("-%d.%02d", whole, minor)
	}

	whole := uint64(m.amount) / 100
	minor := uint64(m.amount) % 100

	return fmt.Sprintf("%d.%02d", whole, minor)
}

func (m Money) checkCurrency(other Money) error {
	if m.currency != other.currency {
		return ErrCurrencyMismatch
	}

	return nil
}

func (m Money) MarshalJSON() ([]byte, error) {
	if !m.currency.Valid() {
		return nil, ErrUnsupportedCurrency
	}

	if m.IsNegative() {
		return nil, ErrNegativeExternal
	}

	return json.Marshal(struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}{
		Amount:   m.String(),
		Currency: string(m.currency),
	})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var raw struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	parsed, err := ParseMoney(raw.Amount, Currency(raw.Currency))
	if err != nil {
		return err
	}

	*m = parsed
	return nil
}
