package money

type Currency string

const (
	BRL Currency = "BRL"
)

func (c Currency) Valid() bool {
	switch c {
	case BRL:
		return true
	default:
		return false
	}
}
