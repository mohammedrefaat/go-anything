package currency

import (
	"testing"
)

func TestConvert(t *testing.T) {
	val, err := Convert(100.0, "USD", "EUR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 92.0 {
		t.Fatalf("expected 92.0, got %f", val)
	}
}

func TestFormat(t *testing.T) {
	res, err := Format(50.5, "USD")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "$50.50" {
		t.Fatalf("expected $50.50, got %s", res)
	}
}
