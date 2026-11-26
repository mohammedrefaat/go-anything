package recorder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/redactor"
	"github.com/mohammedrefaat/go-anything/internal/sandbox"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

func TestIngest_ParseCurl(t *testing.T) {
	curlLine := `curl -X POST "https://api.example.com/v1/orders?ref=123" -H "Authorization: Bearer secret-tok-123" -H "Content-Type: application/json" -d '{"item": "book", "price": 42}'`

	input, err := ParseCurl(curlLine)
	if err != nil {
		t.Fatalf("ParseCurl failed: %v", err)
	}

	if input.Method != "POST" {
		t.Errorf("expected Method POST, got %s", input.Method)
	}

	if input.Path != "/v1/orders?ref=123" {
		t.Errorf("expected Path /v1/orders?ref=123, got %s", input.Path)
	}

	if input.Headers["Authorization"] != "Bearer secret-tok-123" {
		t.Errorf("expected Authorization header, got: %s", input.Headers["Authorization"])
	}

	bodyMap, ok := input.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON body map, got: %T", input.Body)
	}
	if bodyMap["item"] != "book" {
		t.Errorf("expected item=book, got %v", bodyMap["item"])
	}
}

func TestIngest_ParseCLI(t *testing.T) {
	cliLine := `myapp run --name="my job" --verbose 'arg with spaces'`
	input, err := ParseCLI(cliLine)
	if err != nil {
		t.Fatalf("ParseCLI failed: %v", err)
	}

	expected := []string{"myapp", "run", "--name=my job", "--verbose", "arg with spaces"}
	if !reflect.DeepEqual(input.Argv, expected) {
		t.Errorf("expected argv %v, got %v", expected, input.Argv)
	}
}

func TestVolatility_DetectFields(t *testing.T) {
	out1 := &adapterkit.CaseOutput{
		Headers: map[string]string{
			"X-Request-Id": "req-11111",
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"status":    "active",
			"timestamp": "2026-11-19T10:00:00Z",
			"uuid":      "11111111-2222-3333-4444-555555555555",
			"counter":   42,
		},
		Stdout: `{"log_id": "11111111-2222-3333-4444-555555555555"}`,
	}

	out2 := &adapterkit.CaseOutput{
		Headers: map[string]string{
			"X-Request-Id": "req-22222",
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"status":    "active",
			"timestamp": "2026-11-19T10:00:01Z",
			"uuid":      "99999999-8888-7777-6666-555555555555",
			"counter":   42,
		},
		Stdout: `{"log_id": "99999999-8888-7777-6666-555555555555"}`,
	}

	rules := DetectVolatileFields(out1, out2)

	ruleMap := make(map[string]adapterkit.WildcardRule)
	for _, r := range rules {
		ruleMap[r.Path] = r
	}

	// 1. Verify headers.X-Request-Id detected
	if _, ok := ruleMap["headers.X-Request-Id"]; !ok {
		t.Errorf("expected headers.X-Request-Id to be detected as volatile")
	}

	// 2. Verify body.timestamp detected as timestamp
	tsRule, ok := ruleMap["body.timestamp"]
	if !ok || tsRule.Type != "timestamp" {
		t.Errorf("expected body.timestamp with type timestamp, got: %v", tsRule)
	}

	// 3. Verify body.uuid detected as uuid
	uuidRule, ok := ruleMap["body.uuid"]
	if !ok || uuidRule.Type != "uuid" {
		t.Errorf("expected body.uuid with type uuid, got: %v", uuidRule)
	}

	// 4. Verify stdout.log_id detected
	if _, ok := ruleMap["stdout.log_id"]; !ok {
		t.Errorf("expected stdout.log_id to be detected as volatile")
	}

	// 5. Invariant fields (status, counter, Content-Type) should NOT be volatile
	if _, ok := ruleMap["body.status"]; ok {
		t.Errorf("body.status was invariant, should not be wildcarded")
	}
	if _, ok := ruleMap["body.counter"]; ok {
		t.Errorf("body.counter was invariant, should not be wildcarded")
	}
	if _, ok := ruleMap["headers.Content-Type"]; ok {
		t.Errorf("headers.Content-Type was invariant, should not be wildcarded")
	}
}

func TestExitGate_TwoRunsProduceIdenticalOutputWithWildcards(t *testing.T) {
	// Exit Gate: two runs of a command emitting timestamps and UUIDs produce
	// identical redacted case outputs once the generated wildcards are applied.
	run1Output := map[string]any{
		"status":     "success",
		"timestamp":  "2026-11-19T12:00:00.123Z",
		"session_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"code":       200,
	}

	run2Output := map[string]any{
		"status":     "success",
		"timestamp":  "2026-11-19T12:00:01.456Z",
		"session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"code":       200,
	}

	c1 := &adapterkit.CaseOutput{Body: run1Output}
	c2 := &adapterkit.CaseOutput{Body: run2Output}

	wildcards := DetectVolatileFields(c1, c2)
	if len(wildcards) != 2 {
		t.Fatalf("expected 2 detected wildcards, got %d: %v", len(wildcards), wildcards)
	}

	masked1 := ApplyWildcards(run1Output, wildcards)
	masked2 := ApplyWildcards(run2Output, wildcards)

	if !reflect.DeepEqual(masked1, masked2) {
		t.Fatalf("exit gate failure: masked outputs are not identical!\nMasked1: %v\nMasked2: %v", masked1, masked2)
	}
}

func TestRecordCase_DualRunAndJSONLPersistence(t *testing.T) {
	tempWorkspace := t.TempDir()
	store := state.NewStore(tempWorkspace)

	// Initialize layout
	_ = os.MkdirAll(store.CasesDir(), 0755)

	rec := New(Config{
		WorkspaceRoot:  tempWorkspace,
		SourceRevision: "git-commit-hash-abc",
		Runtime:        "go1.22",
		Store:          store,
		Redactor:       redactor.New(),
		SandboxRunner:  sandbox.New(),
	})

	input := adapterkit.CaseInput{
		Method: "POST",
		Path:   "/api/checkout",
		Headers: map[string]string{
			"Authorization": "Bearer mySecretAPIKey12345",
		},
		Body: map[string]any{
			"email": "customer@corporate.internal",
			"card":  "4000 0012 3456 7899",
		},
	}

	// Mock execution function returning differing dynamic values and a secret
	runCount := 0
	mockExec := func(ctx context.Context, in adapterkit.CaseInput) (*adapterkit.CaseOutput, error) {
		runCount++
		ts := "2026-11-19T10:00:00Z"
		uuidVal := "11111111-1111-1111-1111-111111111111"
		if runCount == 2 {
			ts = "2026-11-19T10:00:05Z"
			uuidVal = "22222222-2222-2222-2222-222222222222"
		}

		return &adapterkit.CaseOutput{
			StatusCode: 200,
			Body: map[string]any{
				"tx_id":      uuidVal,
				"created_at": ts,
				"secret_ref": "AKIAIOSFODNN7EXAMPLE",
				"status":     "confirmed",
			},
			Stdout: "payment confirmed",
		}, nil
	}

	res, err := rec.RecordCase(context.Background(), "unit-checkout", input, adapterkit.KindHappy, mockExec)
	if err != nil {
		t.Fatalf("RecordCase failed: %v", err)
	}

	if runCount != 2 {
		t.Fatalf("expected dual-run execution (2 runs), got %d", runCount)
	}

	// 1. Verify wildcards detected
	foundTx := false
	foundCreatedAt := false
	for _, w := range res.Wildcards {
		if w.Path == "body.tx_id" {
			foundTx = true
		}
		if w.Path == "body.created_at" {
			foundCreatedAt = true
		}
	}
	if !foundTx || !foundCreatedAt {
		t.Errorf("expected wildcards for tx_id and created_at, got: %v", res.Wildcards)
	}

	// 2. Verify case persisted to .go-anything/cases/unit-checkout.jsonl
	caseFilePath := filepath.Join(store.CasesDir(), "unit-checkout.jsonl")
	caseData, err := os.ReadFile(caseFilePath)
	if err != nil {
		t.Fatalf("failed to read persisted case file: %v", err)
	}

	caseLines := strings.Split(strings.TrimSpace(string(caseData)), "\n")
	if len(caseLines) != 1 {
		t.Fatalf("expected 1 JSONL case line, got %d", len(caseLines))
	}

	var parsedCase adapterkit.Case
	if err := json.Unmarshal([]byte(caseLines[0]), &parsedCase); err != nil {
		t.Fatalf("persisted case line is not valid JSON: %v", err)
	}

	if parsedCase.UnitID != "unit-checkout" {
		t.Errorf("expected unit-checkout, got %s", parsedCase.UnitID)
	}

	// 3. Verify no raw secrets reached disk
	rawFileContent := string(caseData)
	forbiddenSecrets := []string{
		"mySecretAPIKey12345",
		"customer@corporate.internal",
		"4000 0012 3456 7899",
		"AKIAIOSFODNN7EXAMPLE",
	}

	for _, secret := range forbiddenSecrets {
		if strings.Contains(rawFileContent, secret) {
			t.Errorf("persisted case leaked secret to disk: %q", secret)
		}
	}

	// 4. Verify rules.json was updated with wildcards
	rules, err := store.GetRules()
	if err != nil {
		t.Fatalf("failed to read rules.json: %v", err)
	}
	if len(rules.Wildcards) == 0 {
		t.Errorf("expected rules.json to contain wildcards, got 0")
	}
}
