package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_RecordEffects_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI([]string{"record", "--help"}, &stdout, &stderr)
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "--effects")
	assert.Contains(t, out, "Record external side-effects (outbound HTTP requests and SMTP emails)")
}

func setupEffectsWorkspace(t *testing.T, unitID string) string {
	tempDir := t.TempDir()

	dotGADir := filepath.Join(tempDir, ".go-anything")
	require.NoError(t, os.MkdirAll(dotGADir, 0755))

	manifest := adapterkit.Manifest{
		SchemaVersion:  adapterkit.SchemaVersionV1,
		ProjectName:    "effects-test-workspace",
		SourceLanguage: "go",
		TargetLanguage: "go",
		Units:          []string{unitID},
	}
	mData, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dotGADir, "manifest.json"), mData, 0644))

	casesDir := filepath.Join(dotGADir, "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0755))

	return tempDir
}

// Acceptance Scenario 1: Email effect: A fixture service that sends 1 email.
// When ported accurately, diff passes.
// When modern port sends 2 emails (or misses an email), diff fails with clear message detailing the extra/missing SMTP effect.
func TestCLI_Effects_AcceptanceScenario1_EmailEffect(t *testing.T) {
	unitID := "email_service"
	wsDir := setupEffectsWorkspace(t, unitID)

	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	subject := "Welcome to Platform"
	emailBody := "From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Welcome to Platform\r\n\r\nWelcome Alice!\r\n"

	// Compute expected body hash
	hash := sha256.Sum256([]byte("From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Welcome to Platform\r\n\r\nWelcome Alice!"))
	expectedBodyHash := hex.EncodeToString(hash[:])

	// Baseline case recording 1 email effect
	baselineCase := adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        "case-email-0001",
		UnitID:        unitID,
		Kind:          adapterkit.KindHappy,
		CapturedAt:    "2026-11-23T12:00:00Z",
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/api/v1/welcome",
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: `{"user_id":"u-123","email":"recipient@example.com"}`,
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: `{"status":"email_sent"}`,
			Effects: []adapterkit.Effect{
				{
					Type:         "smtp",
					Order:        1,
					Target:       "127.0.0.1:0",
					EnvelopeFrom: from,
					EnvelopeTo:   to,
					Subject:      subject,
					BodyHash:     expectedBodyHash,
					Count:        1,
				},
			},
		},
	}

	caseBytes, err := json.Marshal(baselineCase)
	require.NoError(t, err)
	casesFile := filepath.Join(wsDir, ".go-anything", "cases", unitID+".jsonl")
	require.NoError(t, os.WriteFile(casesFile, append(caseBytes, '\n'), 0644))

	// Validate baseline effects against schema
	t.Run("validate_effects_schema", func(t *testing.T) {
		effectsPayload, err := json.Marshal(baselineCase.ExpectedOutput.Effects)
		require.NoError(t, err)
		err = schemas.ValidateV2("effects.schema.json", effectsPayload)
		require.NoError(t, err, "generated effects array must validate against effects.schema.json")

		container := map[string]any{
			"schema_version": "v2",
			"unit_id":        unitID,
			"case_id":        baselineCase.CaseID,
			"effects":        baselineCase.ExpectedOutput.Effects,
			"total_effects":  len(baselineCase.ExpectedOutput.Effects),
			"summary":        "baseline email notification",
		}
		containerJSON, err := json.Marshal(container)
		require.NoError(t, err)
		err = schemas.ValidateV2("effects.schema.json", containerJSON)
		require.NoError(t, err, "effects container must validate against effects.schema.json")
	})

	// Test 1: Ported accurately -> Sends 1 matching email -> diff passes
	t.Run("ported_accurately_passes_diff", func(t *testing.T) {
		accurateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			smtpAddr := os.Getenv("SMTP_SERVER")
			if smtpAddr == "" {
				smtpAddr = "127.0.0.1:" + os.Getenv("SMTP_PORT")
			}
			err := smtp.SendMail(smtpAddr, nil, from, to, []byte(emailBody))
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to send email: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"email_sent"}`))
		}))
		defer accurateServer.Close()

		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"diff",
			"--unit=" + unitID,
			"--target=" + accurateServer.URL,
			"--dir=" + wsDir,
		}, &stdout, &stderr)

		require.NoError(t, err, "stderr: %s", stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "Matched: 1")
		assert.Contains(t, out, "Mismatched: 0")
		assert.Contains(t, out, "PASS: 100% replay matched")
	})

	// Test 2: Buggy port sends 2 emails -> Extra SMTP effect divergence -> diff fails with clear message
	t.Run("buggy_port_sends_two_emails_fails_diff_with_extra_effect", func(t *testing.T) {
		buggyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			smtpAddr := os.Getenv("SMTP_SERVER")
			if smtpAddr == "" {
				smtpAddr = "127.0.0.1:" + os.Getenv("SMTP_PORT")
			}
			// Accidental duplicate sending
			_ = smtp.SendMail(smtpAddr, nil, from, to, []byte(emailBody))
			_ = smtp.SendMail(smtpAddr, nil, from, to, []byte(emailBody))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"email_sent"}`))
		}))
		defer buggyServer.Close()

		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"diff",
			"--unit=" + unitID,
			"--target=" + buggyServer.URL,
			"--dir=" + wsDir,
		}, &stdout, &stderr)

		require.Error(t, err)
		assert.Equal(t, ExitCodeFail, GetExitCode(err))

		out := stdout.String()
		assert.Contains(t, out, "Mismatches Detected (1)")
		assert.Contains(t, out, "extra unexpected side effect")
		assert.Contains(t, out, "smtp from=sender@example.com")
		assert.Contains(t, out, "recipient@example.com")
		assert.Contains(t, out, "Welcome to Platform")
	})

	// Test 3: Buggy port misses email (sends 0 emails) -> Missing SMTP effect divergence -> diff fails
	t.Run("buggy_port_misses_email_fails_diff_with_missing_effect", func(t *testing.T) {
		missingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bug: forgotten email sending call
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"email_sent"}`))
		}))
		defer missingServer.Close()

		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"diff",
			"--unit=" + unitID,
			"--target=" + missingServer.URL,
			"--dir=" + wsDir,
		}, &stdout, &stderr)

		require.Error(t, err)
		assert.Equal(t, ExitCodeFail, GetExitCode(err))

		out := stdout.String()
		assert.Contains(t, out, "Mismatches Detected (1)")
		assert.Contains(t, out, "missing expected side effect")
		assert.Contains(t, out, "smtp from=sender@example.com")
		assert.Contains(t, out, "recipient@example.com")
	})
}

// Acceptance Scenario 2: HTTP effect: A fixture service that makes an outbound HTTP call.
// When modern port makes 1 matching outbound HTTP call, diff passes.
// When modern port makes an extra outbound HTTP call, diff fails with clear message detailing the extra HTTP effect.
func TestCLI_Effects_AcceptanceScenario2_HTTPEffect(t *testing.T) {
	unitID := "billing_service"
	wsDir := setupEffectsWorkspace(t, unitID)

	targetEndpoint := "/api/v1/stripe/charges"
	chargePayload := `{"amount":4999,"currency":"usd","customer":"cust_abc123"}`
	hash := sha256.Sum256([]byte(chargePayload))
	expectedHash := hex.EncodeToString(hash[:])

	// Baseline case recording 1 HTTP effect
	baselineCase := adapterkit.Case{
		SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
		CaseID:        "case-charge-0001",
		UnitID:        unitID,
		Kind:          adapterkit.KindHappy,
		CapturedAt:    "2026-11-23T12:00:00Z",
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/checkout",
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: `{"cart_id":"cart_456"}`,
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: `{"order_status":"paid"}`,
			Effects: []adapterkit.Effect{
				{
					Type:     "http",
					Order:    1,
					Target:   targetEndpoint,
					Method:   "POST",
					Headers:  map[string]string{"Content-Type": "application/json"},
					BodyHash: expectedHash,
					Count:    1,
				},
			},
		},
	}

	caseBytes, err := json.Marshal(baselineCase)
	require.NoError(t, err)
	casesFile := filepath.Join(wsDir, ".go-anything", "cases", unitID+".jsonl")
	require.NoError(t, os.WriteFile(casesFile, append(caseBytes, '\n'), 0644))

	// Validate baseline effects against schema
	t.Run("validate_effects_schema", func(t *testing.T) {
		effectsPayload, err := json.Marshal(baselineCase.ExpectedOutput.Effects)
		require.NoError(t, err)
		err = schemas.ValidateV2("effects.schema.json", effectsPayload)
		require.NoError(t, err, "HTTP effects array must validate against effects.schema.json")
	})

	// Test 1: Ported accurately -> Makes 1 matching outbound HTTP call -> diff passes
	t.Run("ported_accurately_passes_diff", func(t *testing.T) {
		accurateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHTTP := os.Getenv("MOCK_HTTP_URL")
			if mockHTTP == "" {
				mockHTTP = os.Getenv("TARGET_HTTP_URL")
			}

			// Perform 1 outbound HTTP call to external stripe payment gateway
			resp, err := http.Post(mockHTTP+targetEndpoint, "application/json", strings.NewReader(chargePayload))
			if err != nil {
				http.Error(w, fmt.Sprintf("outbound HTTP call failed: %v", err), http.StatusInternalServerError)
				return
			}
			defer resp.Body.Close()
			_, _ = io.ReadAll(resp.Body)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"order_status":"paid"}`))
		}))
		defer accurateServer.Close()

		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"diff",
			"--unit=" + unitID,
			"--target=" + accurateServer.URL,
			"--dir=" + wsDir,
		}, &stdout, &stderr)

		require.NoError(t, err, "stderr: %s", stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "Matched: 1")
		assert.Contains(t, out, "Mismatched: 0")
		assert.Contains(t, out, "PASS: 100% replay matched")
	})

	// Test 2: Buggy port makes 2 outbound HTTP calls -> Extra HTTP effect divergence -> diff fails with clear message
	t.Run("buggy_port_makes_extra_outbound_http_fails_diff", func(t *testing.T) {
		buggyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHTTP := os.Getenv("MOCK_HTTP_URL")
			if mockHTTP == "" {
				mockHTTP = os.Getenv("TARGET_HTTP_URL")
			}

			// Bug: duplicated outbound charge call (double-billing!)
			resp1, _ := http.Post(mockHTTP+targetEndpoint, "application/json", strings.NewReader(chargePayload))
			if resp1 != nil {
				resp1.Body.Close()
			}
			resp2, _ := http.Post(mockHTTP+targetEndpoint, "application/json", strings.NewReader(chargePayload))
			if resp2 != nil {
				resp2.Body.Close()
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"order_status":"paid"}`))
		}))
		defer buggyServer.Close()

		var stdout, stderr bytes.Buffer
		err := runCLI([]string{
			"diff",
			"--unit=" + unitID,
			"--target=" + buggyServer.URL,
			"--dir=" + wsDir,
		}, &stdout, &stderr)

		require.Error(t, err)
		assert.Equal(t, ExitCodeFail, GetExitCode(err))

		out := stdout.String()
		assert.Contains(t, out, "Mismatches Detected (1)")
		assert.Contains(t, out, "extra unexpected side effect")
		assert.Contains(t, out, "http POST /api/v1/stripe/charges")
	})
}

func TestCLI_Record_EffectsFlag_Command(t *testing.T) {
	unitID := "worker"
	wsDir := setupEffectsWorkspace(t, unitID)

	src := `package main

import (
	"net/http"
	"net/smtp"
	"os"
	"strings"
)

func main() {
	smtpAddr := os.Getenv("SMTP_SERVER")
	if smtpAddr == "" {
		smtpAddr = "127.0.0.1:" + os.Getenv("SMTP_PORT")
	}
	if smtpAddr != "" && smtpAddr != "127.0.0.1:" {
		_ = smtp.SendMail(smtpAddr, nil, "bot@example.com", []string{"dev@example.com"}, []byte("Subject: Worker Done\r\n\r\nDone"))
	}

	httpTarget := os.Getenv("MOCK_HTTP_URL")
	if httpTarget != "" {
		_, _ = http.Post(httpTarget+"/worker/ping", "application/json", strings.NewReader("{\"status\":\"ok\"}"))
	}
}
`
	helperSrc := filepath.Join(wsDir, "main.go")
	require.NoError(t, os.WriteFile(helperSrc, []byte(src), 0644))

	binName := "helper"
	if os.PathSeparator == '\\' {
		binName += ".exe"
	}
	helperBin := filepath.Join(wsDir, binName)
	buildCmd := exec.Command("go", "build", "-o", helperBin, helperSrc)
	require.NoError(t, buildCmd.Run())

	var stdout, stderr bytes.Buffer
	err := runCLI([]string{
		"record",
		"--unit=" + unitID,
		"--effects",
		"--cmd=" + filepath.ToSlash(helperBin),
		"--dir=" + wsDir,
	}, &stdout, &stderr)
	require.NoError(t, err, "stderr: %s", stderr.String())

	out := stdout.String()
	assert.Contains(t, out, "Recorded 1 case(s)")
	assert.Contains(t, out, "Recorded 2 side effect(s)")

	// Read recorded case
	casesFile := filepath.Join(wsDir, ".go-anything", "cases", unitID+".jsonl")
	data, err := os.ReadFile(casesFile)
	require.NoError(t, err)

	var recordedCase adapterkit.Case
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &recordedCase))

	require.Len(t, recordedCase.ExpectedOutput.Effects, 2)
	assert.Equal(t, "smtp", recordedCase.ExpectedOutput.Effects[0].Type)
	assert.Equal(t, "bot@example.com", recordedCase.ExpectedOutput.Effects[0].EnvelopeFrom)
	assert.Equal(t, "http", recordedCase.ExpectedOutput.Effects[1].Type)
	assert.Contains(t, recordedCase.ExpectedOutput.Effects[1].Target, "/worker/ping")

	// Validate with schema
	effectsJSON, err := json.Marshal(recordedCase.ExpectedOutput.Effects)
	require.NoError(t, err)
	err = schemas.ValidateV2("effects.schema.json", effectsJSON)
	require.NoError(t, err)
}
