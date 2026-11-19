package billing

import "testing"

func TestProcessPayment(t *testing.T) {
	receipt, err := ProcessPayment(100, "EUR", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt != "Charged €100.00 successfully" {
		t.Fatalf("expected receipt, got %s", receipt)
	}

	_, err = ProcessPayment(1000, "USD", true)
	if err == nil {
		t.Fatalf("expected fraud detection error")
	}
}
