package fraud

// Evaluate checks if a transaction should be flagged for risk review.
func Evaluate(amountUSD float64, highRisk bool) bool {
	if highRisk && amountUSD > 500 {
		return true
	}
	return amountUSD > 10000
}
