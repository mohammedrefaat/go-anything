package redactor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectAndRedact_NewDetectors(t *testing.T) {
	r := New()

	type testCase struct {
		name        string
		input       string
		wantMatch   bool
		wantKind    Kind
		secretValue string // raw secret that must not appear after RedactString
		wantContain string // format-preserving substring expected after RedactString
	}

	cases := []testCase{
		// Google API key (>= 3 positive, >= 3 negative)
		{
			name:        "google_api_key_positive_standalone",
			input:       "key=AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe",
			wantMatch:   true,
			wantKind:    KindGoogleAPIKey,
			secretValue: "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe",
			wantContain: "AIza",
		},
		{
			name:        "google_api_key_positive_quoted",
			input:       `GOOGLE_API_KEY="AIzaSyB1234567890abcdefghijklmnopqrstuv"`,
			wantMatch:   true,
			wantKind:    KindGoogleAPIKey,
			secretValue: "AIzaSyB1234567890abcdefghijklmnopqrstuv",
			wantContain: `GOOGLE_API_KEY="AIza`,
		},
		{
			name:        "google_api_key_positive_url_query",
			input:       "https://maps.googleapis.com/maps/api/js?key=AIzaSyC_9876543210ZYXWVUTSRQPONMLKJIHGF",
			wantMatch:   true,
			wantKind:    KindGoogleAPIKey,
			secretValue: "AIzaSyC_9876543210ZYXWVUTSRQPONMLKJIHGF",
			wantContain: "?key=AIza",
		},
		{
			name:      "google_api_key_negative_too_short",
			input:     "key=AIzaShort123",
			wantMatch: false,
		},
		{
			name:      "google_api_key_negative_too_long",
			input:     "key=AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQeExtra",
			wantMatch: false,
		},
		{
			name:      "google_api_key_negative_no_boundary",
			input:     "prefixAIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe",
			wantMatch: false,
		},

		// AWS secret access key (>= 3 positive, >= 3 negative)
		{
			name:        "aws_secret_positive_near_akia",
			input:       "AKIAIOSFODNN7EXAMPLE:wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantMatch:   true,
			wantKind:    KindAWSSecretKey,
			secretValue: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantContain: "AKIA",
		},
		{
			name:        "aws_secret_positive_near_key_name",
			input:       `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
			wantMatch:   true,
			wantKind:    KindAWSSecretKey,
			secretValue: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantContain: `aws_secret_access_key = "`,
		},
		{
			name:        "aws_secret_positive_json_near_akia",
			input:       `{"accessKeyId": "AKIAIOSFODNN7EXAMPLE", "secretAccessKey": "9b2cf535f27731c974343645a3985328884c1234"}`,
			wantMatch:   true,
			wantKind:    KindAWSSecretKey,
			secretValue: "9b2cf535f27731c974343645a3985328884c1234",
			wantContain: "AKIA",
		},
		{
			name:      "aws_secret_negative_bare_40_chars",
			input:     "commit_digest: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantMatch: false,
		},
		{
			name:      "aws_secret_negative_outside_window",
			input:     "AKIAIOSFODNN7EXAMPLE " + strings.Repeat("padding ", 30) + "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantMatch: false,
			wantKind:  KindAWSSecretKey,
		},
		{
			name:      "aws_secret_negative_wrong_length_near_anchor",
			input:     "aws_secret_access_key=shortval",
			wantMatch: false,
		},

		// URL credentials (>= 3 positive, >= 3 negative)
		{
			name:        "url_cred_positive_https",
			input:       "connect to https://alice:supersecret123@api.internal:8443/v1/items",
			wantMatch:   true,
			wantKind:    KindURLCredential,
			secretValue: "supersecret123",
			wantContain: "https://alice:",
		},
		{
			name:        "url_cred_positive_postgres",
			input:       "postgres://dbadmin:P4ssw0rd99@db.prod.internal:5432/app?sslmode=require",
			wantMatch:   true,
			wantKind:    KindURLCredential,
			secretValue: "P4ssw0rd99",
			wantContain: "@db.prod.internal:5432/app?sslmode=require",
		},
		{
			name:        "url_cred_positive_redis",
			input:       "redis://default:r3disSecr3t@cache.internal:6379/0",
			wantMatch:   true,
			wantKind:    KindURLCredential,
			secretValue: "r3disSecr3t",
			wantContain: "redis://default:",
		},
		{
			name:      "url_cred_negative_port_only",
			input:     "https://api.internal:8443/v1/items",
			wantMatch: false,
			wantKind:  KindURLCredential,
		},
		{
			name:      "url_cred_negative_user_only",
			input:     "https://alice@localhost/v1",
			wantMatch: false,
			wantKind:  KindURLCredential,
		},
		{
			name:      "url_cred_negative_port_with_at_in_path",
			input:     "https://example.com:8080/users/profile",
			wantMatch: false,
			wantKind:  KindURLCredential,
		},

		// Generic assignment (>= 3 positive, >= 3 negative)
		{
			name:        "generic_positive_password_eq",
			input:       `password = "s3cur3P@ssw0rd!"`,
			wantMatch:   true,
			wantKind:    KindGenericAssignment,
			secretValue: "s3cur3P@ssw0rd!",
			wantContain: `password = "`,
		},
		{
			name:        "generic_positive_client_secret_colon",
			input:       `client_secret: 'my-prod-client-secret-99'`,
			wantMatch:   true,
			wantKind:    KindGenericAssignment,
			secretValue: "my-prod-client-secret-99",
			wantContain: `client_secret: '`,
		},
		{
			name:        "generic_positive_json_api_key",
			input:       `"api_key": "live_9876543210abcdef"`,
			wantMatch:   true,
			wantKind:    KindGenericAssignment,
			secretValue: "live_9876543210abcdef",
			wantContain: `"api_key": "`,
		},
		{
			name:      "generic_negative_changeme",
			input:     `password = "changeme123"`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_example",
			input:     `api_key = "example_api_key_value"`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_xxx",
			input:     `token = "xxxxxxxxxxxx"`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_angle_placeholder",
			input:     `secret = "<your-secret-here>"`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_env_var_placeholder",
			input:     `client_secret = "${CLIENT_SECRET}"`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_os_getenv",
			input:     `api_key = os.Getenv("API_KEY")`,
			wantMatch: false,
		},
		{
			name:      "generic_negative_short_value",
			input:     `password = "short"`,
			wantMatch: false,
		},

		// SendGrid (>= 3 positive, >= 3 negative)
		{
			name:        "sendgrid_positive_1",
			input:       "SG" + ".abcdefghijklmnop123456." + "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefg",
			wantMatch:   true,
			wantKind:    KindSendGridKey,
			secretValue: "SG" + ".abcdefghijklmnop123456." + "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefg",
			wantContain: "SG.",
		},
		{
			name:        "sendgrid_positive_2",
			input:       `SENDGRID_API_KEY="SG.1234567890_abcdefghijk.1234567890_abcdefghijklmnopqrstuvwxyz012345"`,
			wantMatch:   true,
			wantKind:    KindSendGridKey,
			secretValue: "SG.1234567890_abcdefghijk.1234567890_abcdefghijklmnopqrstuvwxyz012345",
			wantContain: "SG.",
		},
		{
			name:        "sendgrid_positive_3",
			input:       "Bearer SG.a-b_c-d_e-f_g-h_i-j_12.a-b_c-d_e-f_g-h_i-j_k-l_m-n_o-p_q-r_s-t_u-v",
			wantMatch:   true,
			wantKind:    KindSendGridKey,
			secretValue: "SG.a-b_c-d_e-f_g-h_i-j_12.a-b_c-d_e-f_g-h_i-j_k-l_m-n_o-p_q-r_s-t_u-v",
			wantContain: "Bearer SG.",
		},
		{
			name:      "sendgrid_negative_short_part1",
			input:     "SG.short.ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefg",
			wantMatch: false,
		},
		{
			name:      "sendgrid_negative_short_part2",
			input:     "SG.abcdefghijklmnop123456.short",
			wantMatch: false,
		},
		{
			name:      "sendgrid_negative_wrong_prefix",
			input:     "XG.abcdefghijklmnop123456.ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefg",
			wantMatch: false,
		},

		// npm (>= 3 positive, >= 3 negative)
		{
			name:        "npm_positive_1",
			input:       "npm_1234567890abcdefghijklmnopqrstuvwxyz",
			wantMatch:   true,
			wantKind:    KindNPMToken,
			secretValue: "npm_1234567890abcdefghijklmnopqrstuvwxyz",
			wantContain: "npm_",
		},
		{
			name:        "npm_positive_2",
			input:       "//registry.npmjs.org/:_authToken=npm_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
			wantMatch:   true,
			wantKind:    KindNPMToken,
			secretValue: "npm_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
			wantContain: "_authToken=npm_",
		},
		{
			name:        "npm_positive_3",
			input:       `NPM_TOKEN="npm_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8"`,
			wantMatch:   true,
			wantKind:    KindNPMToken,
			secretValue: "npm_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8",
			wantContain: `"npm_`,
		},
		{
			name:      "npm_negative_too_short",
			input:     "npm_1234567890abcdef",
			wantMatch: false,
		},
		{
			name:      "npm_negative_underscore_inside",
			input:     "npm_1234567890_bcdefghijklmnopqrstuvwxyz",
			wantMatch: false,
		},
		{
			name:      "npm_negative_too_long",
			input:     "npm_1234567890abcdefghijklmnopqrstuvwxyz99",
			wantMatch: false,
		},

		// Twilio (>= 3 positive, >= 3 negative)
		{
			name:        "twilio_positive_1",
			input:       "SK" + "0123456789abcdef0123456789abcdef",
			wantMatch:   true,
			wantKind:    KindTwilioKey,
			secretValue: "SK" + "0123456789abcdef0123456789abcdef",
			wantContain: "SK",
		},
		{
			name:        "twilio_positive_2",
			input:       `TWILIO_API_KEY="` + "SK" + `deadbeefcafebabe0123456789abcdef"`,
			wantMatch:   true,
			wantKind:    KindTwilioKey,
			secretValue: "SK" + "deadbeefcafebabe0123456789abcdef",
			wantContain: `"SK`,
		},
		{
			name:        "twilio_positive_3",
			input:       "sid=" + "SK" + "fedcba9876543210fedcba9876543210",
			wantMatch:   true,
			wantKind:    KindTwilioKey,
			secretValue: "SK" + "fedcba9876543210fedcba9876543210",
			wantContain: "sid=SK",
		},
		{
			name:      "twilio_negative_too_short",
			input:     "SK0123456789abcdef",
			wantMatch: false,
		},
		{
			name:      "twilio_negative_non_hex",
			input:     "SK0123456789abcdef0123456789abcdeg",
			wantMatch: false,
		},
		{
			name:      "twilio_negative_uppercase_hex",
			input:     "SK" + "0123456789ABCDEF0123456789ABCDEF",
			wantMatch: false,
		},

		// Tightened sk-... detector (>= 3 positive, >= 3 negative)
		{
			name:        "sk_positive_anthropic",
			input:       "key=sk-ant-api03-1234567890abcdef123456",
			wantMatch:   true,
			wantKind:    KindAnthropicKey,
			secretValue: "sk-ant-api03-1234567890abcdef123456",
			wantContain: "sk-",
		},
		{
			name:        "sk_positive_openai_proj",
			input:       "key=sk-proj-abcdef1234567890abcdef",
			wantMatch:   true,
			wantKind:    KindAnthropicKey,
			secretValue: "sk-proj-abcdef1234567890abcdef",
			wantContain: "sk-",
		},
		{
			name:        "sk_positive_legacy_long",
			input:       "key=sk-1234567890abcdef1234567890abcdef",
			wantMatch:   true,
			wantKind:    KindAnthropicKey,
			secretValue: "sk-1234567890abcdef1234567890abcdef",
			wantContain: "sk-",
		},
		{
			name:      "sk_negative_sklearn",
			input:     "using sk-learn-pipeline for preprocessing",
			wantMatch: false,
		},
		{
			name:      "sk_negative_css_class",
			input:     "class=sk-button-primary",
			wantMatch: false,
		},
		{
			name:      "sk_negative_locale_pack",
			input:     "loaded sk-translation-pack-v2",
			wantMatch: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Detect(tc.input)
			if !tc.wantMatch {
				for _, f := range findings {
					if tc.wantKind == "" || f.Kind == tc.wantKind {
						t.Fatalf("expected no match for %q, got finding %+v (%q)", tc.input, f, tc.input[f.Start:f.End])
					}
				}
				redacted, _, err := r.RedactString(tc.input)
				require.NoError(t, err)
				if tc.wantKind == KindAWSSecretKey {
					assert.Contains(t, redacted, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
				} else {
					assert.Equal(t, tc.input, redacted)
				}
				return
			}

			var found *Finding
			for i := range findings {
				if findings[i].Kind == tc.wantKind {
					found = &findings[i]
					break
				}
			}
			require.NotNil(t, found, "expected finding of kind %s in %q, got %+v", tc.wantKind, tc.input, findings)
			assert.Equal(t, tc.secretValue, tc.input[found.Start:found.End])

			redacted, _, err := r.RedactString(tc.input)
			require.NoError(t, err)
			assert.NotContains(t, redacted, tc.secretValue)
			assert.Contains(t, redacted, tc.wantContain)
			assert.Equal(t, len(tc.input), len(redacted), "substitution must preserve length")
		})
	}
}

func TestDetect_PureAndNonOverlapping(t *testing.T) {
	input := "https://alice:mySecretPass99@db.internal:5432/app"
	orig := strings.Clone(input)

	findings := Detect(input)
	assert.Equal(t, orig, input, "Detect must not mutate input")
	require.Len(t, findings, 1)
	assert.Equal(t, KindURLCredential, findings[0].Kind)
	assert.Equal(t, "mySecretPass99", input[findings[0].Start:findings[0].End])

	assert.Nil(t, Detect(""))
}

func TestRedactCase_PlantedSecretsE2E(t *testing.T) {
	r := New()

	plantedSecrets := []string{
		"AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"superSecretDbPass99",
		"topSecretAssignmentVal",
		"SG" + ".abcdefghijklmnop123456." + "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890abcdefg",
		"npm_1234567890abcdefghijklmnopqrstuvwxyz",
		"SK" + "0123456789abcdef0123456789abcdef",
		"sk-ant-api03-1234567890abcdef123456",
	}

	c := &adapterkit.Case{
		SchemaVersion:  "v1alpha1",
		CaseID:         "case-planted-001",
		UnitID:         "unit-secrets",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "deadbeef",
		Runtime:        "go1.27",
		CapturedAt:     "2026-11-25T08:00:00Z",
		Input: adapterkit.CaseInput{
			Method: "POST",
			Path:   "/v1/sync?key=" + plantedSecrets[0],
			Headers: map[string]string{
				"X-SendGrid": "Bearer " + plantedSecrets[4],
			},
			Body: map[string]any{
				"dsn":                   "postgres://alice:" + plantedSecrets[2] + "@db.internal:5432/app",
				"npm":                   plantedSecrets[5],
				"twilio":                plantedSecrets[6],
				"anth":                  plantedSecrets[7],
				"aws_access_key_id":     "AKIAIOSFODNN7EXAMPLE",
				"aws_secret_access_key": plantedSecrets[1],
			},
			Argv: []string{"--config", `client_secret = "` + plantedSecrets[3] + `"`},
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: 200,
			Stdout:     "connected to postgres://alice:" + plantedSecrets[2] + "@db.internal:5432/app AKIAIOSFODNN7EXAMPLE:" + plantedSecrets[1],
		},
	}

	redacted, err := r.RedactCase(c)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	casesDir := filepath.Join(tmpDir, ".go-anything", "cases")
	require.NoError(t, os.MkdirAll(casesDir, 0o755))

	casePath := filepath.Join(casesDir, "unit-secrets.jsonl")
	line, err := json.Marshal(redacted)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(casePath, append(line, '\n'), 0o600))

	diskBytes, err := os.ReadFile(casePath)
	require.NoError(t, err)
	diskContent := string(diskBytes)

	for _, secret := range plantedSecrets {
		assert.NotContains(t, diskContent, secret, "raw secret leaked to .go-anything/cases/*.jsonl")
	}
	assert.Contains(t, diskContent, "@db.internal:5432/app", "URL host/port/path should be preserved")
	assert.Contains(t, diskContent, "AIza")
	assert.Contains(t, diskContent, "SG.")
	assert.Contains(t, diskContent, "npm_")
	assert.Contains(t, diskContent, "SK")

	// Fail-closed check: unparseable JSON with a planted secret must error and write nothing.
	corruptCase := &adapterkit.Case{
		Input: adapterkit.CaseInput{
			Body: `{"secret": "` + plantedSecrets[0] + `"`,
		},
	}
	_, err = r.RedactCase(corruptCase)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnparseablePayload))
}
