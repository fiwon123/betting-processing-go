package money

import "math"

type Money struct {
	amount int64
	currency    Currency
}

func NewMoney(amount int64, currency Currency) (Money, error) {
	if !currency.Valid() {
		return Money{}, ErrUnsupportedCurrency
	}

	return Money{
		amount: amount,
		currency:    currency,
	}, nil
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
		amount: m.amount + other.amount,
		currency:    m.currency,
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
		amount: -other.amount,
		currency:    other.currency,
	})
}

func (m Money) Negate() (Money, error) {
	if m.amount == math.MinInt64 {
		return Money{}, ErrOverflow
	}

	return Money{
		amount: -m.amount,
		currency:    m.currency,
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

func (m Money) IsNegative() bool {
	return m.amount < 0
}

func (m Money) IsZero() bool {
	return m.amount == 0
}

func (m Money) checkCurrency(other Money) error {
	if m.currency != other.currency {
		return ErrCurrencyMismatch
	}

	return nil
}
