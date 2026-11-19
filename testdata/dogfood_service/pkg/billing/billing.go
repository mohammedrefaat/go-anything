package billing

import (
	"fmt"

	"example.com/payments/pkg/currency"
	"example.com/payments/pkg/fraud"
)

// ProcessPayment converts amount to USD, executes fraud checks, and returns formatted receipt.
func ProcessPayment(amount float64, curr string, highRisk bool) (string, error) {
	convertedUSD, err := currency.Convert(amount, curr, "USD")
	if err != nil {
		return "", err
	}

	if fraud.Evaluate(convertedUSD, highRisk) {
		return "", fmt.Errorf("transaction flagged by fraud detection")
	}

	formatted, err := currency.Format(amount, curr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Charged %s successfully", formatted), nil
}
