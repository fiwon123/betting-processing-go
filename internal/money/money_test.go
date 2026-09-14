package money

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func mustMoney(t *testing.T, amount int64) Money {
	t.Helper()

	m, err := NewMoney(amount, BRL)
	if err != nil {
		t.Fatalf("NewMoney(%d, BRL) unexpected error: %v", amount, err)
	}

	return m
}

func TestParseMoneyValid(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0.00", 0},
		{"1.00", 100},
		{"10.50", 1050},
		{"100", 10000},
		{"0.01", 1},
		{"999999.99", 99999999},
		{"25", 2500},
		{"25.0", 2500},
		{"25.00", 2500},
		{"1.5", 150},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m, err := ParseMoney(tt.input, BRL)
			if err != nil {
				t.Fatalf("ParseMoney(%q) unexpected error: %v", tt.input, err)
			}

			if m.Amount() != tt.expected {
				t.Errorf(
					"ParseMoney(%q) = %d, want %d",
					tt.input,
					m.Amount(),
					tt.expected,
				)
			}

			if m.Currency() != BRL {
				t.Errorf(
					"ParseMoney(%q) currency = %s, want %s",
					tt.input,
					m.Currency(),
					BRL,
				)
			}
		})
	}
}

func TestParseMoneyRejectsInvalid(t *testing.T) {
	invalid := []string{
		"",
		" ",
		" 1.00",
		"1.00 ",
		"\t1.00",
		"\n1.00",
		"1 00",
		"abc",
		"-1.00",
		"+1.00",
		"1.234",
		"1,00",
		"1e10",
		"1E10",
		"1+1",
		"1.23.45",
		"NaN",
		"Inf",
		"Infinity",
		"1.",
		".50",
		"R$1.00",
		"1_00",
		"--1",
		"++1",
		"-+1",
		"1-0",
	}

	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			_, err := ParseMoney(input, BRL)
			if err == nil {
				t.Errorf("ParseMoney(%q) expected error, got nil", input)
			}
		})
	}
}

func TestParseMoneyOverflow(t *testing.T) {
	tests := []struct {
		input string
		want  error
	}{
		{"92233720368547758.07", nil},
		{"92233720368547758.08", ErrOverflow},
		{"92233720368547759", ErrOverflow},
		{"999999999999999999999999", ErrOverflow},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseMoney(tt.input, BRL)

			if err != tt.want {
				t.Errorf(
					"ParseMoney(%q) error = %v, want %v",
					tt.input,
					err,
					tt.want,
				)
			}
		})
	}
}

func TestParseMoneyRejectsUnsupportedCurrency(t *testing.T) {
	_, err := ParseMoney("10.00", Currency("USD"))
	if err != ErrUnsupportedCurrency {
		t.Errorf("ParseMoney error = %v, want %v", err, ErrUnsupportedCurrency)
	}
}

func TestNewMoneyValid(t *testing.T) {
	m, err := NewMoney(1000, BRL)
	if err != nil {
		t.Fatalf("NewMoney(1000, BRL) unexpected error: %v", err)
	}

	if m.Amount() != 1000 {
		t.Errorf("Amount() = %d, want 1000", m.Amount())
	}

	if m.Currency() != BRL {
		t.Errorf("Currency() = %s, want BRL", m.Currency())
	}
}

func TestNewMoneyAllowsNegativeInternalValue(t *testing.T) {
	m, err := NewMoney(-1000, BRL)
	if err != nil {
		t.Fatalf("NewMoney(-1000, BRL) unexpected error: %v", err)
	}

	if !m.IsNegative() {
		t.Error("expected negative money")
	}
}

func TestNewMoneyRejectsInvalidCurrency(t *testing.T) {
	_, err := NewMoney(1000, Currency("USD"))
	if err != ErrUnsupportedCurrency {
		t.Errorf("error = %v, want %v", err, ErrUnsupportedCurrency)
	}
}

func TestZeroMoney(t *testing.T) {
	z, err := ZeroMoney(BRL)
	if err != nil {
		t.Fatalf("ZeroMoney unexpected error: %v", err)
	}

	if !z.IsZero() {
		t.Error("expected zero money")
	}

	if z.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", z.Currency(), BRL)
	}
}

func TestZeroMoneyRejectsInvalidCurrency(t *testing.T) {
	_, err := ZeroMoney(Currency("USD"))
	if err != ErrUnsupportedCurrency {
		t.Errorf("error = %v, want %v", err, ErrUnsupportedCurrency)
	}
}

func TestMoneyAdd(t *testing.T) {
	a := mustMoney(t, 1000)
	b := mustMoney(t, 500)

	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add unexpected error: %v", err)
	}

	if result.Amount() != 1500 {
		t.Errorf("Add() = %d, want 1500", result.Amount())
	}

	if result.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", result.Currency(), BRL)
	}
}

func TestMoneyAddNegativeValues(t *testing.T) {
	a := mustMoney(t, 1000)
	b := mustMoney(t, -300)

	result, err := a.Add(b)
	if err != nil {
		t.Fatalf("Add unexpected error: %v", err)
	}

	if result.Amount() != 700 {
		t.Errorf("Add() = %d, want 700", result.Amount())
	}
}

func TestMoneySubtract(t *testing.T) {
	a := mustMoney(t, 1000)
	b := mustMoney(t, 300)

	result, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("Subtract unexpected error: %v", err)
	}

	if result.Amount() != 700 {
		t.Errorf("Subtract() = %d, want 700", result.Amount())
	}
}

func TestMoneySubtractNegative(t *testing.T) {
	a := mustMoney(t, 1000)
	b := mustMoney(t, -300)

	result, err := a.Subtract(b)
	if err != nil {
		t.Fatalf("Subtract unexpected error: %v", err)
	}

	if result.Amount() != 1300 {
		t.Errorf("Subtract() = %d, want 1300", result.Amount())
	}
}

func TestMoneyNegate(t *testing.T) {
	m := mustMoney(t, 500)

	neg, err := m.Negate()
	if err != nil {
		t.Fatalf("Negate unexpected error: %v", err)
	}

	if neg.Amount() != -500 {
		t.Errorf("Negate() = %d, want -500", neg.Amount())
	}

	if neg.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", neg.Currency(), BRL)
	}
}

func TestMoneyCompare(t *testing.T) {
	a := mustMoney(t, 1000)
	b := mustMoney(t, 500)
	c := mustMoney(t, 1000)

	tests := []struct {
		name string
		x    Money
		y    Money
		want int
	}{
		{"greater", a, b, 1},
		{"less", b, a, -1},
		{"equal", a, c, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.x.Compare(tt.y)
			if err != nil {
				t.Fatalf("Compare unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("Compare() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMoneyIsZero(t *testing.T) {
	z := mustMoney(t, 0)
	if !z.IsZero() {
		t.Error("expected IsZero to return true")
	}

	nonZero := mustMoney(t, 100)
	if nonZero.IsZero() {
		t.Error("expected IsZero to return false")
	}
}

func TestMoneyIsNegative(t *testing.T) {
	neg := mustMoney(t, -100)
	if !neg.IsNegative() {
		t.Error("expected IsNegative to return true")
	}

	positive := mustMoney(t, 100)
	if positive.IsNegative() {
		t.Error("expected IsNegative to return false")
	}

	zero := mustMoney(t, 0)
	if zero.IsNegative() {
		t.Error("expected zero to not be negative")
	}
}

func TestMoneyString(t *testing.T) {
	tests := []struct {
		amount   int64
		expected string
	}{
		{0, "0.00"},
		{1, "0.01"},
		{50, "0.50"},
		{100, "1.00"},
		{1050, "10.50"},
		{math.MaxInt64, "92233720368547758.07"},
		{-1, "-0.01"},
		{-50, "-0.50"},
		{-100, "-1.00"},
		{-1050, "-10.50"},
		{math.MinInt64, "-92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			m := mustMoney(t, tt.amount)

			if got := m.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMustParseValid(t *testing.T) {
	m := MustParse("25.00", BRL)

	if m.Amount() != 2500 {
		t.Errorf("MustParse('25.00') = %d, want 2500", m.Amount())
	}

	if m.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", m.Currency(), BRL)
	}
}

func TestMustParseInvalidPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid input")
		}
	}()

	MustParse("invalid", BRL)
}

func TestMoneyAddPositiveOverflow(t *testing.T) {
	max := mustMoney(t, math.MaxInt64-100)
	big := mustMoney(t, 200)

	_, err := max.Add(big)
	if err != ErrOverflow {
		t.Errorf("expected ErrOverflow on add, got %v", err)
	}
}

func TestMoneyAddNegativeOverflow(t *testing.T) {
	min := mustMoney(t, math.MinInt64+100)
	negative := mustMoney(t, -200)

	_, err := min.Add(negative)
	if err != ErrOverflow {
		t.Errorf("expected ErrOverflow on add, got %v", err)
	}
}

func TestMoneySubtractPositiveOverflow(t *testing.T) {
	min := mustMoney(t, math.MinInt64)
	one := mustMoney(t, 1)

	_, err := min.Subtract(one)
	if err != ErrOverflow {
		t.Errorf("expected ErrOverflow on subtract, got %v", err)
	}
}

func TestMoneySubtractNegativeOverflow(t *testing.T) {
	max := mustMoney(t, math.MaxInt64-100)
	negative := mustMoney(t, -200)

	_, err := max.Subtract(negative)
	if err != ErrOverflow {
		t.Errorf("expected ErrOverflow on subtract, got %v", err)
	}
}

func TestMoneyNegateMinInt64(t *testing.T) {
	m := mustMoney(t, math.MinInt64)

	_, err := m.Negate()
	if err != ErrOverflow {
		t.Errorf("Negate() error = %v, want %v", err, ErrOverflow)
	}
}

func TestMoneyJSONMarshal(t *testing.T) {
	m := mustMoney(t, 2500)

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal unexpected error: %v", err)
	}

	expected := `{"amount":"25.00","currency":"BRL"}`
	if string(data) != expected {
		t.Errorf("json.Marshal() = %s, want %s", data, expected)
	}
}

func TestMoneyJSONMarshalRejectsNegative(t *testing.T) {
	m := mustMoney(t, -2500)

	if _, err := json.Marshal(m); err == nil {
		t.Error("expected error when marshaling negative money")
	}
}

func TestMoneyJSONMarshalRejectsInvalidCurrency(t *testing.T) {
	m := Money{
		amount:   1000,
		currency: Currency("USD"),
	}

	_, err := json.Marshal(m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnsupportedCurrency) {
		t.Errorf("error = %v, want %v", err, ErrUnsupportedCurrency)
	}
}

func TestMoneyJSONUnmarshal(t *testing.T) {
	input := `{"amount":"25.00","currency":"BRL"}`

	var m Money
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("json.Unmarshal unexpected error: %v", err)
	}

	if m.Amount() != 2500 {
		t.Errorf("Amount() = %d, want 2500", m.Amount())
	}

	if m.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", m.Currency(), BRL)
	}
}

func TestMoneyJSONRejectsNegative(t *testing.T) {
	input := `{"amount":"-25.00","currency":"BRL"}`

	var m Money
	if err := json.Unmarshal([]byte(input), &m); err == nil {
		t.Error("expected error for negative JSON amount")
	}
}

func TestMoneyJSONRejectsInvalidInput(t *testing.T) {
	tests := []string{
		`{}`,
		`{"amount":"25.00"}`,
		`{"currency":"BRL"}`,
		`{"amount":25.00,"currency":"BRL"}`,
		`{"amount":"25.00","currency":"USD"}`,
		`{"amount":"","currency":"BRL"}`,
		`{"amount":"25.00","currency":""}`,
		`{"amount":"25.000","currency":"BRL"}`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			var m Money

			if err := json.Unmarshal([]byte(input), &m); err == nil {
				t.Errorf("expected error for JSON %s", input)
			}
		})
	}
}

func TestMoneyJSONUnmarshalPreservesValueOnError(t *testing.T) {
	m := mustMoney(t, 1000)

	err := json.Unmarshal(
		[]byte(`{"amount":"invalid","currency":"BRL"}`),
		&m,
	)
	if err == nil {
		t.Fatal("expected error")
	}

	if m.Amount() != 1000 {
		t.Errorf("Amount() = %d, want 1000", m.Amount())
	}

	if m.Currency() != BRL {
		t.Errorf("Currency() = %s, want %s", m.Currency(), BRL)
	}
}
