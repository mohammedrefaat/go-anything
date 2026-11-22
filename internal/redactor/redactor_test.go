package redactor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

func TestLuhnValidationAndSubstitution(t *testing.T) {
	// Known valid test card numbers
	cards := []struct {
		network string
		raw     string
	}{
		{"Visa", "4000 0012 3456 7899"},
		{"Visa-hyphen", "4000-0012-3456-7899"},
		{"Mastercard", "5500-0000-0000-0004"},
		{"Amex", "378282246310005"},
		{"Discover", "6011000990139424"},
	}

	for _, c := range cards {
		t.Run(c.network, func(t *testing.T) {
			if !IsValidLuhn(c.raw) {
				t.Fatalf("card %s should be Luhn-valid", c.raw)
			}

			sub := SubstituteCreditCard(c.raw)
			if sub == c.raw {
				t.Fatalf("substituted card should differ from original raw card")
			}

			if !IsValidLuhn(sub) {
				t.Fatalf("substituted card %s is not Luhn-valid", sub)
			}

			if len(sub) != len(c.raw) {
				t.Fatalf("length mismatch: expected %d, got %d", len(c.raw), len(sub))
			}

			// Verify formatting (spaces and hyphens in identical positions)
			for i := range c.raw {
				if c.raw[i] == ' ' || c.raw[i] == '-' {
					if sub[i] != c.raw[i] {
						t.Errorf("expected separator %q at pos %d, got %q", c.raw[i], i, sub[i])
					}
				}
			}
		})
	}
}

func TestRedactString_Detectors(t *testing.T) {
	r := New()

	tests := []struct {
		name         string
		input        string
		wantNotMatch string
		wantContains string
	}{
		{
			name:         "GitHub personal access token",
			input:        "Token is ghp_1234567890abcdef1234567890abcdef1234 end",
			wantNotMatch: "ghp_1234567890abcdef1234567890abcdef1234",
			wantContains: "ghp_",
		},
		{
			name:         "AWS Access Key ID",
			input:        "AWS key: AKIAIOSFODNN7EXAMPLE in config",
			wantNotMatch: "AKIAIOSFODNN7EXAMPLE",
			wantContains: "AKIA",
		},
		{
			name:         "Stripe test key",
			input:        "Stripe key: sk_test_fakekey123456789012345678",
			wantNotMatch: "sk_test_fakekey123456789012345678",
			wantContains: "sk_test_",
		},
		{
			name:         "Slack token",
			input:        "Slack: xoxb-1234567890-abcdef123456",
			wantNotMatch: "xoxb-1234567890-abcdef123456",
			wantContains: "xoxb-",
		},
		{
			name:         "JWT token",
			input:        "Auth: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			wantNotMatch: "SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			wantContains: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
		{
			name:         "PEM RSA private key",
			input:        "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0Y1234567890abcdef\n-----END RSA PRIVATE KEY-----",
			wantNotMatch: "0Y1234567890abcdef",
			wantContains: "-----BEGIN RSA PRIVATE KEY-----",
		},
		{
			name:         "Email address to reserved domain",
			input:        "Contact us at alice.smith+support@mycompany.internal for info",
			wantNotMatch: "alice.smith+support@mycompany.internal",
			wantContains: "@example.com",
		},
		{
			name:         "Credit card number",
			input:        "Paid with 4000 0012 3456 7899 on terminal",
			wantNotMatch: "4000 0012 3456 7899",
			wantContains: "4242 4242",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := r.RedactString(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(got, tc.wantNotMatch) {
				t.Errorf("output still contains secret %q: got %q", tc.wantNotMatch, got)
			}
			if !strings.Contains(got, tc.wantContains) {
				t.Errorf("output does not contain expected format %q: got %q", tc.wantContains, got)
			}
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	r := New()

	headers := map[string]string{
		"Authorization":       "Bearer mySuperSecretToken12345",
		"Cookie":              "session=supersecret_session_id; Path=/; Secure; HttpOnly",
		"X-Hub-Signature-256": "sha256=d57c2923e5c3fc68771991dd134137ea47ab214d",
		"Content-Type":        "application/json",
		"X-API-Key":           "secret_api_key_value",
	}

	redacted, unrep, err := r.RedactHeaders(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Authorization header redacted
	if strings.Contains(redacted["Authorization"], "mySuperSecretToken12345") {
		t.Errorf("Authorization header secret not redacted: %s", redacted["Authorization"])
	}
	if !strings.HasPrefix(redacted["Authorization"], "Bearer ") {
		t.Errorf("Authorization header lost Bearer prefix: %s", redacted["Authorization"])
	}

	// 2. Cookie values redacted but attributes preserved
	if strings.Contains(redacted["Cookie"], "supersecret_session_id") {
		t.Errorf("Cookie secret not redacted: %s", redacted["Cookie"])
	}
	if !strings.Contains(redacted["Cookie"], "Path=/") || !strings.Contains(redacted["Cookie"], "HttpOnly") {
		t.Errorf("Cookie lost attributes: %s", redacted["Cookie"])
	}

	// 3. Signature header flagged as unreplayable
	if redacted["X-Hub-Signature-256"] != "[unreplayable]" {
		t.Errorf("expected [unreplayable], got %s", redacted["X-Hub-Signature-256"])
	}
	foundUnrep := false
	for _, u := range unrep {
		if u == "X-Hub-Signature-256" {
			foundUnrep = true
			break
		}
	}
	if !foundUnrep {
		t.Errorf("X-Hub-Signature-256 was not recorded in unreplayable fields: %v", unrep)
	}

	// 4. Normal headers preserved
	if redacted["Content-Type"] != "application/json" {
		t.Errorf("Content-Type modified unexpectedly: %s", redacted["Content-Type"])
	}

	// 5. Secret header redacted
	if strings.Contains(redacted["X-API-Key"], "secret_api_key_value") {
		t.Errorf("X-API-Key secret not redacted: %s", redacted["X-API-Key"])
	}
}

func TestRedactJSON_FailClosed(t *testing.T) {
	rDefault := New() // fail-closed by default

	invalidJSON := []byte(`{"user": "alice", "broken": `)

	// 1. Must fail closed on invalid JSON
	_, _, err := rDefault.RedactJSON(invalidJSON)
	if err == nil {
		t.Fatalf("expected error on unparseable JSON payload, got nil")
	}
	if !errors.Is(err, ErrUnparseablePayload) {
		t.Errorf("expected ErrUnparseablePayload, got: %v", err)
	}

	// 2. With UnsafeAllowUnredacted, allows text-fallback
	rUnsafe := New(Options{UnsafeAllowUnredacted: true})
	out, _, err := rUnsafe.RedactJSON(invalidJSON)
	if err != nil {
		t.Fatalf("unexpected error with UnsafeAllowUnredacted: %v", err)
	}
	if len(out) == 0 {
		t.Errorf("expected non-empty output")
	}

	// 3. Valid JSON with secrets and signature
	validJSON := []byte(`{
		"user": "john.doe@company.internal",
		"api_key": "secret_key_12345",
		"card": "4000 0012 3456 7899",
		"signature": "hmac_sha256_hash_value"
	}`)

	redactedBytes, unrep, err := rDefault.RedactJSON(validJSON)
	if err != nil {
		t.Fatalf("unexpected error on valid JSON: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(redactedBytes, &parsed); err != nil {
		t.Fatalf("failed to re-parse redacted JSON: %v", err)
	}

	if strings.Contains(parsed["user"].(string), "company.internal") {
		t.Errorf("email domain not redacted: %v", parsed["user"])
	}
	if strings.Contains(parsed["api_key"].(string), "secret_key_12345") {
		t.Errorf("api_key not redacted: %v", parsed["api_key"])
	}
	if parsed["card"].(string) == "4000 0012 3456 7899" || !IsValidLuhn(parsed["card"].(string)) {
		t.Errorf("card not substituted or invalid: %v", parsed["card"])
	}
	if parsed["signature"].(string) != "[unreplayable]" {
		t.Errorf("signature should be [unreplayable], got: %v", parsed["signature"])
	}

	foundSig := false
	for _, u := range unrep {
		if u == "signature" {
			foundSig = true
			break
		}
	}
	if !foundSig {
		t.Errorf("signature field not flagged in unreplayable: %v", unrep)
	}
}

func TestRedactCase_FullPipeline(t *testing.T) {
	r := New()

	originalCase := &adapterkit.Case{
		SchemaVersion:  "v1alpha1",
		CaseID:         "case-001",
		UnitID:         "unit-auth",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "git-commit-hash-1234",
		Runtime:        "python3.11",
		CapturedAt:     "2026-11-19T10:00:00Z",
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/api/checkout",
			Headers: map[string]string{
				"Authorization":       "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
				"X-Hub-Signature-256": "sha256=abcdef1234567890",
				"Content-Type":        "application/json",
			},
			Body: `{"email": "buyer@megacorp.internal", "card": "4000-0012-3456-7899", "signature": "sig_mac_123"}`,
			Env: map[string]string{
				"STRIPE_KEY": "sk_test_fakekey123456789012345678",
				"PATH":       "/usr/bin:/bin",
			},
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: 200,
			Headers: map[string]string{
				"Set-Cookie": "session=auth_secret_session_token_xyz; Path=/; HttpOnly; Secure",
			},
			Body: map[string]any{
				"status":    "success",
				"tx_id":     "tx_123456",
				"token":     "ghp_1234567890abcdef1234567890abcdef1234",
				"mac_check": "calculated_mac_val",
			},
			Stdout: "Transaction verified for buyer@megacorp.internal\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0Y1234567890abcdef\n-----END RSA PRIVATE KEY-----",
		},
	}

	redactedCase, err := r.RedactCase(originalCase)
	if err != nil {
		t.Fatalf("RedactCase failed: %v", err)
	}

	// 1. Verify secrets never reach disk (no raw secrets remain)
	rawCaseBytes, err := json.Marshal(redactedCase)
	if err != nil {
		t.Fatalf("failed to marshal redacted case: %v", err)
	}
	caseStr := string(rawCaseBytes)

	forbiddenStrings := []string{
		"buyer@megacorp.internal",
		"4000-0012-3456-7899",
		"sk_test_fakekey123456789012345678",
		"auth_secret_session_token_xyz",
		"ghp_1234567890abcdef1234567890abcdef1234",
		"0Y1234567890abcdef",
		"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
	}

	for _, forbidden := range forbiddenStrings {
		if strings.Contains(caseStr, forbidden) {
			t.Errorf("redacted case leaked forbidden secret string: %q", forbidden)
		}
	}

	// 2. Verify format-preserving substitutions
	if !strings.Contains(caseStr, "@example.com") {
		t.Errorf("expected email to use reserved domain @example.com")
	}
	if !strings.Contains(caseStr, "sk_test_") {
		t.Errorf("expected Stripe key to preserve sk_test_ prefix")
	}
	if !strings.Contains(caseStr, "ghp_") {
		t.Errorf("expected GitHub token to preserve ghp_ prefix")
	}

	// 3. Verify unreplayable fields are flagged
	expectedUnreplayable := []string{
		"expected_output.body.mac_check",
		"input.body.signature",
		"input.headers.X-Hub-Signature-256",
	}

	for _, expected := range expectedUnreplayable {
		found := false
		for _, actual := range redactedCase.UnreplayableFields {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected unreplayable field %q not found in Case.UnreplayableFields: %v", expected, redactedCase.UnreplayableFields)
		}
	}

	// 4. Verify unparseable case body fails closed
	corruptCase := &adapterkit.Case{
		Input: adapterkit.CaseInput{
			Body: `{"corrupt": [`,
		},
	}
	_, err = r.RedactCase(corruptCase)
	if err == nil || !errors.Is(err, ErrUnparseablePayload) {
		t.Errorf("expected ErrUnparseablePayload for corrupt case body, got %v", err)
	}
}

func TestRedactCase_PathAndMetadataAndNestedJSON(t *testing.T) {
	r := New()

	c := &adapterkit.Case{
		Input: adapterkit.CaseInput{
			Path: "/auth?token=ghp_012345678901234567890123456789012345",
			Argv: []string{"--key", "ghp_012345678901234567890123456789012346"},
		},
		Metadata: map[string]any{
			"api_key":       "my-super-secret-key-12345",
			"nested_config": `{"password": "secretPassword123", "normal": "hello"}`,
		},
	}

	redacted, err := r.RedactCase(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. Path must not contain raw token
	if strings.Contains(redacted.Input.Path, "ghp_012345678901234567890123456789012345") {
		t.Errorf("leaked raw token in Input.Path: %s", redacted.Input.Path)
	}

	// 2. Argv must not contain raw token
	if strings.Contains(redacted.Input.Argv[1], "ghp_012345678901234567890123456789012346") {
		t.Errorf("leaked raw token in Input.Argv: %v", redacted.Input.Argv)
	}

	// 3. Metadata secret field must be redacted
	if redacted.Metadata["api_key"] == "my-super-secret-key-12345" {
		t.Errorf("metadata api_key was not redacted")
	}

	// 4. Nested JSON string inside metadata must have password redacted
	nestedStr, ok := redacted.Metadata["nested_config"].(string)
	if !ok {
		t.Fatalf("expected nested_config to be string, got %T", redacted.Metadata["nested_config"])
	}
	if strings.Contains(nestedStr, "secretPassword123") {
		t.Errorf("nested JSON password was not redacted: %s", nestedStr)
	}
	if !strings.Contains(nestedStr, "normal") {
		t.Errorf("nested JSON non-secret field lost: %s", nestedStr)
	}
}
