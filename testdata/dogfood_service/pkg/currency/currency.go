package currency

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrUnsupportedCurrency = errors.New("unsupported currency code")
	ErrNegativeAmount      = errors.New("amount cannot be negative")
)

var exchangeRates = map[string]float64{
	"USD": 1.0,
	"EUR": 0.92,
	"GBP": 0.79,
	"JPY": 155.0,
}

// Convert converts an amount from one currency to another using fixed exchange rates.
func Convert(amount float64, from, to string) (float64, error) {
	if amount < 0 {
		return 0, ErrNegativeAmount
	}
	rateFrom, okFrom := exchangeRates[from]
	if !okFrom {
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedCurrency, from)
	}
	rateTo, okTo := exchangeRates[to]
	if !okTo {
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedCurrency, to)
	}

	inUSD := amount / rateFrom
	result := inUSD * rateTo
	return math.Round(result*100) / 100, nil
}

// Format formats an amount with its currency symbol.
func Format(amount float64, curr string) (string, error) {
	if _, ok := exchangeRates[curr]; !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCurrency, curr)
	}
	switch curr {
	case "USD":
		return fmt.Sprintf("$%.2f", amount), nil
	case "EUR":
		return fmt.Sprintf("€%.2f", amount), nil
	case "GBP":
		return fmt.Sprintf("£%.2f", amount), nil
	case "JPY":
		return fmt.Sprintf("¥%.0f", amount), nil
	default:
		return fmt.Sprintf("%.2f %s", amount, curr), nil
	}
}
