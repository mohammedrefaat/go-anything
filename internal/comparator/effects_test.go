package comparator_test

import (
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/comparator"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModeEffectMultiset_HTTPMatch(t *testing.T) {
	c := comparator.New()

	eff1 := adapterkit.Effect{
		Type:     "http",
		Target:   "http://127.0.0.1:8080/api/v1/charges",
		Method:   "POST",
		Headers:  map[string]string{"Content-Type": "application/json", "X-Custom": "val1"},
		BodyHash: "hash-charge-1",
	}
	eff2 := adapterkit.Effect{
		Type:     "http",
		Target:   "http://127.0.0.1:8080/api/v1/receipts",
		Method:   "POST",
		Headers:  map[string]string{"Content-Type": "application/json"},
		BodyHash: "hash-receipt-1",
	}

	t.Run("identical effects in same order", func(t *testing.T) {
		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{eff1, eff2}, []adapterkit.Effect{eff1, eff2})
		require.NoError(t, err)
		assert.True(t, res.Passed)
		assert.Empty(t, res.Mismatches)
	})

	t.Run("multiset order independence", func(t *testing.T) {
		// eff2 before eff1 in actual
		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{eff1, eff2}, []adapterkit.Effect{eff2, eff1})
		require.NoError(t, err)
		assert.True(t, res.Passed)
		assert.Empty(t, res.Mismatches)
	})

	t.Run("loopback ephemeral port independence", func(t *testing.T) {
		actEff1 := eff1
		actEff1.Target = "http://127.0.0.1:54321/api/v1/charges"
		actEff2 := eff2
		actEff2.Target = "http://127.0.0.1:54321/api/v1/receipts"

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{eff1, eff2}, []adapterkit.Effect{actEff1, actEff2})
		require.NoError(t, err)
		assert.True(t, res.Passed)
		assert.Empty(t, res.Mismatches)
	})
}

func TestModeEffectMultiset_SMTPMatch(t *testing.T) {
	c := comparator.New()

	expSMTP := adapterkit.Effect{
		Type:         "smtp",
		EnvelopeFrom: "billing@example.com",
		EnvelopeTo:   []string{"alice@example.com", "bob@example.com"},
		Subject:      "Monthly Statement",
		BodyHash:     "hash-stmt-1",
	}

	t.Run("identical SMTP effect", func(t *testing.T) {
		actSMTP := expSMTP
		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{expSMTP}, []adapterkit.Effect{actSMTP})
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})

	t.Run("SMTP recipient order permutation matches", func(t *testing.T) {
		actSMTP := expSMTP
		actSMTP.EnvelopeTo = []string{"bob@example.com", "alice@example.com"}

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{expSMTP}, []adapterkit.Effect{actSMTP})
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})

	t.Run("case-insensitive envelope from and recipient match", func(t *testing.T) {
		actSMTP := expSMTP
		actSMTP.EnvelopeFrom = "BILLING@EXAMPLE.COM"
		actSMTP.EnvelopeTo = []string{"Alice@Example.COM", "BOB@example.com"}

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{expSMTP}, []adapterkit.Effect{actSMTP})
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})
}

func TestModeEffectMultiset_VolatileHeadersIgnored(t *testing.T) {
	c := comparator.New()

	expEff := adapterkit.Effect{
		Type:   "http",
		Target: "http://127.0.0.1:8080/api/v1/notify",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Date":          "Mon, 23 Nov 2026 12:00:00 GMT",
			"Message-ID":    "<msg-123@example.com>",
			"Authorization": "Bearer token-expected",
			"X-Request-Id":  "req-uuid-1",
		},
		BodyHash: "hash-notify-1",
	}

	actEff := adapterkit.Effect{
		Type:   "http",
		Target: "http://127.0.0.1:8080/api/v1/notify",
		Method: "POST",
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Date":          "Mon, 23 Nov 2026 12:00:05 GMT",
			"Message-ID":    "<msg-999@example.com>",
			"Authorization": "Bearer token-actual-different",
			"X-Request-Id":  "req-uuid-2",
		},
		BodyHash: "hash-notify-1",
	}

	t.Run("volatile headers difference does not fail", func(t *testing.T) {
		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{expEff}, []adapterkit.Effect{actEff})
		require.NoError(t, err)
		assert.True(t, res.Passed)
		assert.Empty(t, res.Mismatches)
	})

	t.Run("non-volatile header mismatch fails", func(t *testing.T) {
		actDiff := actEff
		actDiff.Headers = map[string]string{
			"Content-Type": "text/plain", // changed!
		}
		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{expEff}, []adapterkit.Effect{actDiff})
		require.NoError(t, err)
		assert.False(t, res.Passed)
		assert.NotEmpty(t, res.Mismatches)
	})
}

func TestModeEffectMultiset_ExtraEffectDivergence(t *testing.T) {
	c := comparator.New()

	chargeEff := adapterkit.Effect{
		Type:     "http",
		Target:   "/api/v1/charges",
		Method:   "POST",
		BodyHash: "hash-charge",
	}

	t.Run("extra HTTP call detected as divergence", func(t *testing.T) {
		// Expected 1 charge, but actual performed 2 charges (double-billing divergence!)
		expected := []adapterkit.Effect{chargeEff}
		actual := []adapterkit.Effect{chargeEff, chargeEff}

		res, err := c.Compare(comparator.ModeEffectMultiset, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "extra unexpected side effect")
		assert.Contains(t, res.Mismatches[0].Actual, "http POST /api/v1/charges")
	})

	t.Run("extra SMTP email detected as divergence", func(t *testing.T) {
		emailEff := adapterkit.Effect{
			Type:         "smtp",
			EnvelopeFrom: "support@example.com",
			EnvelopeTo:   []string{"user@example.com"},
			Subject:      "Welcome",
		}
		expected := []adapterkit.Effect{}
		actual := []adapterkit.Effect{emailEff}

		res, err := c.Compare(comparator.ModeEffectMultiset, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "extra unexpected side effect")
		assert.Contains(t, res.Mismatches[0].Actual, "smtp from=support@example.com")
	})
}

func TestModeEffectMultiset_MissingEffectDivergence(t *testing.T) {
	c := comparator.New()

	emailEff := adapterkit.Effect{
		Type:         "smtp",
		EnvelopeFrom: "billing@example.com",
		EnvelopeTo:   []string{"alice@example.com"},
		Subject:      "Receipt",
		BodyHash:     "hash-receipt",
	}

	t.Run("missing SMTP email detected as divergence", func(t *testing.T) {
		expected := []adapterkit.Effect{emailEff}
		actual := []adapterkit.Effect{} // port failed to send email

		res, err := c.Compare(comparator.ModeEffectMultiset, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "missing expected side effect")
		assert.Contains(t, res.Mismatches[0].Expected, "smtp from=billing@example.com")
	})

	t.Run("missing HTTP call detected as divergence", func(t *testing.T) {
		httpEff := adapterkit.Effect{
			Type:     "http",
			Target:   "/api/v1/analytics",
			Method:   "POST",
			BodyHash: "hash-analytics",
		}
		expected := []adapterkit.Effect{httpEff}
		actual := []adapterkit.Effect{}

		res, err := c.Compare(comparator.ModeEffectMultiset, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "missing expected side effect")
		assert.Contains(t, res.Mismatches[0].Expected, "http POST /api/v1/analytics")
	})
}

func TestModeEffectMultiset_DetailedFieldMismatches(t *testing.T) {
	c := comparator.New()

	t.Run("body hash mismatch explanation", func(t *testing.T) {
		exp := adapterkit.Effect{
			Type:     "http",
			Target:   "/api/v1/data",
			Method:   "POST",
			BodyHash: "hash-expected",
		}
		act := adapterkit.Effect{
			Type:     "http",
			Target:   "/api/v1/data",
			Method:   "POST",
			BodyHash: "hash-different",
		}

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{exp}, []adapterkit.Effect{act})
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.NotEmpty(t, res.Mismatches)
		assert.Contains(t, res.Mismatches[0].Message, "body hash mismatch")
	})

	t.Run("HTTP method mismatch explanation", func(t *testing.T) {
		exp := adapterkit.Effect{
			Type:   "http",
			Target: "/api/v1/data",
			Method: "POST",
		}
		act := adapterkit.Effect{
			Type:   "http",
			Target: "/api/v1/data",
			Method: "PUT",
		}

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{exp}, []adapterkit.Effect{act})
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.NotEmpty(t, res.Mismatches)
		assert.Contains(t, res.Mismatches[0].Message, "method mismatch")
	})

	t.Run("SMTP subject mismatch explanation", func(t *testing.T) {
		exp := adapterkit.Effect{
			Type:         "smtp",
			EnvelopeFrom: "info@example.com",
			EnvelopeTo:   []string{"user@example.com"},
			Subject:      "Welcome to Service",
		}
		act := adapterkit.Effect{
			Type:         "smtp",
			EnvelopeFrom: "info@example.com",
			EnvelopeTo:   []string{"user@example.com"},
			Subject:      "Incorrect Subject",
		}

		res, err := c.Compare(comparator.ModeEffectMultiset, []adapterkit.Effect{exp}, []adapterkit.Effect{act})
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.NotEmpty(t, res.Mismatches)
		assert.Contains(t, res.Mismatches[0].Message, "subject mismatch")
	})
}

func TestCompareCaseOutput_WithEffects(t *testing.T) {
	c := comparator.New()

	eff := adapterkit.Effect{
		Type:     "http",
		Target:   "/webhook",
		Method:   "POST",
		BodyHash: "hash-wh",
	}

	t.Run("HTTPContract passes when body and effects match", func(t *testing.T) {
		exp := &adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       `{"status":"ok"}`,
			Effects:    []adapterkit.Effect{eff},
		}
		act := &adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       `{"status":"ok"}`,
			Effects:    []adapterkit.Effect{eff},
		}

		res, err := c.CompareCaseOutput(comparator.ModeHTTPContract, exp, act)
		require.NoError(t, err)
		assert.True(t, res.Passed)
		assert.Empty(t, res.Mismatches)
	})

	t.Run("HTTPContract fails when body matches but extra effect produced", func(t *testing.T) {
		exp := &adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       `{"status":"ok"}`,
			Effects:    []adapterkit.Effect{eff},
		}
		act := &adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       `{"status":"ok"}`,
			Effects:    []adapterkit.Effect{eff, eff}, // duplicate effect!
		}

		res, err := c.CompareCaseOutput(comparator.ModeHTTPContract, exp, act)
		require.NoError(t, err)
		assert.False(t, res.Passed)
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "extra unexpected side effect")
	})

	t.Run("CompareCaseOutput directly with ModeEffectMultiset", func(t *testing.T) {
		exp := &adapterkit.CaseOutput{
			Effects: []adapterkit.Effect{eff},
		}
		act := &adapterkit.CaseOutput{
			Effects: []adapterkit.Effect{eff},
		}

		res, err := c.CompareCaseOutput(comparator.ModeEffectMultiset, exp, act)
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})
}
