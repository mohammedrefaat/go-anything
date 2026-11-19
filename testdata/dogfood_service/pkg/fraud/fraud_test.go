package fraud

import "testing"

func TestEvaluate(t *testing.T) {
	if !Evaluate(600, true) {
		t.Fatalf("expected high risk over 500 to be flagged")
	}
	if Evaluate(400, true) {
		t.Fatalf("expected 400 not to be flagged")
	}
	if !Evaluate(15000, false) {
		t.Fatalf("expected over 10000 to be flagged")
	}
}
