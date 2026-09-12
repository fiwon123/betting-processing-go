package money

import "math"

type Money struct {
	amountMinor int64
	currency    Currency
}

func NewMoney(amountMinor int64, currency Currency) (Money, error) {
	if !currency.Valid() {
		return Money{}, ErrUnsupportedCurrency
	}

	return Money{
		amountMinor: amountMinor,
		currency:    currency,
	}, nil
}

func ZeroMoney(currency Currency) (Money, error) {
	return NewMoney(0, currency)
}

func (m Money) AmountMinor() int64 {
	return m.amountMinor
}

func (m Money) Currency() Currency {
	return m.currency
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	if other.amountMinor > 0 &&
		m.amountMinor > math.MaxInt64-other.amountMinor {
		return Money{}, ErrOverflow
	}

	if other.amountMinor < 0 &&
		m.amountMinor < math.MinInt64-other.amountMinor {
		return Money{}, ErrOverflow
	}

	return Money{
		amountMinor: m.amountMinor + other.amountMinor,
		currency:    m.currency,
	}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if err := m.checkCurrency(other); err != nil {
		return Money{}, err
	}

	if other.amountMinor == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return m.Add(Money{
		amountMinor: -other.amountMinor,
		currency:    other.currency,
	})
}

func (m Money) Negate() (Money, error) {
	if m.amountMinor == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return Money{
		amountMinor: -m.amountMinor,
		currency:    m.currency,
	}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if err := m.checkCurrency(other); err != nil {
		return 0, err
	}

	switch {
	case m.amountMinor < other.amountMinor:
		return -1, nil
	case m.amountMinor > other.amountMinor:
		return 1, nil
	default:
		return 0, nil
	}
}

func (m Money) IsNegative() bool {
	return m.amountMinor < 0
}

func (m Money) IsZero() bool {
	return m.amountMinor == 0
}

func (m Money) checkCurrency(other Money) error {
	if m.currency != other.currency {
		return ErrCurrencyMismatch
	}

	return nil
}
