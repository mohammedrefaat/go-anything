package adapterhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// TestHelperAdapterProcess simulates an external adapter speaking JSON RPC over stdio.
func TestHelperAdapterProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ADAPTER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req RPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := RPCResponse{Error: "malformed request JSON"}
			data, _ := json.Marshal(resp)
			fmt.Println(string(data))
			continue
		}

		var resp RPCResponse
		resp.ID = req.ID

		switch req.Method {
		case "ping":
			resp.Result = json.RawMessage(`"pong"`)
		case "analyze":
			resp.Result = json.RawMessage(`{"status":"analyzed","units":["unit-1"]}`)
		case "fail_op":
			resp.Error = "simulated adapter error"
		default:
			resp.Result = json.RawMessage(`{"method":"` + req.Method + `"}`)
		}

		data, _ := json.Marshal(resp)
		fmt.Println(string(data))
	}
}

func TestAdapterHost_ChecksumVerification(t *testing.T) {
	tempDir := t.TempDir()
	adapterPath := filepath.Join(tempDir, "mock_adapter.sh")

	content := []byte("#!/bin/sh\necho 'mock adapter'")
	if err := os.WriteFile(adapterPath, content, 0755); err != nil {
		t.Fatalf("failed to write mock adapter: %v", err)
	}

	trueChecksum, err := ComputeSHA256(adapterPath)
	if err != nil {
		t.Fatalf("ComputeSHA256 failed: %v", err)
	}

	// 1. Valid checksum passes
	if err := VerifyChecksum(adapterPath, trueChecksum); err != nil {
		t.Errorf("expected checksum verification to pass, got: %v", err)
	}

	// 2. Tampered binary fails checksum
	tamperedContent := append(content, []byte("\n# tampered payload")...)
	if err := os.WriteFile(adapterPath, tamperedContent, 0755); err != nil {
		t.Fatalf("failed to tamper adapter: %v", err)
	}

	err = VerifyChecksum(adapterPath, trueChecksum)
	if err == nil {
		t.Fatalf("expected tampered adapter to fail checksum verification")
	}
	if !errors.Is(err, ErrTamperedAdapter) {
		t.Errorf("expected ErrTamperedAdapter, got: %v", err)
	}

	// 3. Non-existent adapter returns ErrAdapterNotInstalled
	nonExistent := filepath.Join(tempDir, "non_existent.bin")
	err = VerifyChecksum(nonExistent, trueChecksum)
	if !errors.Is(err, ErrAdapterNotInstalled) {
		t.Errorf("expected ErrAdapterNotInstalled for missing file, got: %v", err)
	}
}

func TestAdapterHost_CapabilityEnforcement(t *testing.T) {
	// Adapter declares only CapabilityReadRepo
	cfg := AdapterConfig{
		Name:         "test-adapter",
		Executable:   os.Args[0],
		Args:         []string{"-test.run=TestHelperAdapterProcess"},
		Capabilities: []adapterkit.Capability{adapterkit.CapabilityReadRepo},
	}

	// 1. Invoking with undeclared CapabilityRunOriginal must be blocked
	transport := &SubprocessTransport{cfg: cfg}

	ctx := context.Background()
	_, err := transport.InvokeWithCapability(ctx, adapterkit.CapabilityRunOriginal, "run_original", nil)
	if err == nil {
		t.Fatalf("expected undeclared capability to be rejected, got nil")
	}
	if !errors.Is(err, adapterkit.ErrUndeclaredCapability) {
		t.Errorf("expected ErrUndeclaredCapability, got: %v", err)
	}

	// 2. Invoking with undeclared CapabilityNetwork must be blocked
	_, err = transport.InvokeWithCapability(ctx, adapterkit.CapabilityNetwork, "network_fetch", nil)
	if !errors.Is(err, adapterkit.ErrUndeclaredCapability) {
		t.Errorf("expected ErrUndeclaredCapability for network, got: %v", err)
	}
}

func TestAdapterHost_JSONStdioTransport(t *testing.T) {
	trueChecksum, err := ComputeSHA256(os.Args[0])
	if err != nil {
		t.Fatalf("failed to compute test binary checksum: %v", err)
	}

	cfg := AdapterConfig{
		Name:           "helper-adapter",
		Executable:     os.Args[0],
		Args:           []string{"-test.run=TestHelperAdapterProcess"},
		SHA256Checksum: trueChecksum,
		Capabilities: []adapterkit.Capability{
			adapterkit.CapabilityReadRepo,
			adapterkit.CapabilityRunOriginal,
		},
	}

	// Inject environment variable to trigger helper process
	_ = os.Setenv("GO_WANT_ADAPTER_PROCESS", "1")
	defer func() {
		_ = os.Unsetenv("GO_WANT_ADAPTER_PROCESS")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	transport, err := NewSubprocessTransport(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to spawn subprocess transport: %v", err)
	}
	defer transport.Close()

	if transport.Type() != adapterkit.TransportSubprocess {
		t.Errorf("expected TransportSubprocess, got %s", transport.Type())
	}

	// 1. Invoke analyze (requires declared CapabilityReadRepo)
	resBytes, err := transport.Invoke(ctx, "analyze", nil)
	if err != nil {
		t.Fatalf("Invoke('analyze') failed: %v", err)
	}
	var resMap map[string]any
	if err := json.Unmarshal(resBytes, &resMap); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if resMap["status"] != "analyzed" {
		t.Errorf("unexpected analyze response: %v", resMap)
	}

	// 2. Invoke method that returns error from adapter
	_, err = transport.InvokeWithCapability(ctx, adapterkit.CapabilityRunOriginal, "fail_op", nil)
	if err == nil {
		t.Fatalf("expected error from fail_op, got nil")
	}
	if !strings.Contains(err.Error(), "simulated adapter error") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDoctor_CheckGitignore(t *testing.T) {
	tempDir := t.TempDir()
	engine := NewDoctorEngine(tempDir)

	// 1. Missing .gitignore produces error
	issues := engine.CheckGitignore()
	if len(issues) == 0 {
		t.Fatalf("expected issue for missing .gitignore")
	}
	if issues[0].Severity != StatusError {
		t.Errorf("expected error severity for missing .gitignore, got %s", issues[0].Severity)
	}

	// 2. .gitignore without local/ produces error
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	_ = os.WriteFile(gitignorePath, []byte("node_modules/\n*.log\n"), 0644)

	issues = engine.CheckGitignore()
	hasLocalError := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, ".go-anything/local/") {
			hasLocalError = true
			break
		}
	}
	if !hasLocalError {
		t.Errorf("expected missing .go-anything/local/ error in issues: %v", issues)
	}

	// 3. Complete .gitignore passes cleanly
	completeGitignore := ".go-anything/local/\n.go-anything/captures/\n*.capture\n"
	_ = os.WriteFile(gitignorePath, []byte(completeGitignore), 0644)

	issues = engine.CheckGitignore()
	if len(issues) != 0 {
		t.Errorf("expected clean gitignore check, got issues: %v", issues)
	}
}

func TestDoctor_CheckCommittedSecrets(t *testing.T) {
	tempDir := t.TempDir()
	engine := NewDoctorEngine(tempDir)

	// 1. Clean repo has no secrets
	cleanFile := filepath.Join(tempDir, "config.json")
	_ = os.WriteFile(cleanFile, []byte(`{"app": "go-anything", "port": 8080}`), 0644)

	issues := engine.CheckCommittedSecrets()
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues in clean repo, got: %v", issues)
	}

	// 2. Leaked AWS key detected
	leakedAWSFile := filepath.Join(tempDir, "aws_credentials.txt")
	_ = os.WriteFile(leakedAWSFile, []byte("aws_access_key_id = AKIAIOSFODNN7EXAMPLE\n"), 0644)

	issues = engine.CheckCommittedSecrets()
	if len(issues) == 0 {
		t.Fatalf("expected doctor to catch leaked AWS secret key")
	}
	foundAWS := false
	for _, issue := range issues {
		if strings.Contains(issue.Path, "aws_credentials.txt") {
			foundAWS = true
			if issue.Severity != StatusError {
				t.Errorf("expected StatusError for leaked secret, got: %s", issue.Severity)
			}
		}
	}
	if !foundAWS {
		t.Errorf("expected issue for aws_credentials.txt, got issues: %v", issues)
	}
}

func TestDoctor_CheckUnsafeFlags(t *testing.T) {
	engine := NewDoctorEngine(".")

	// 1. Unsafe redaction bypass flag produces error
	issues := engine.CheckUnsafeFlags([]string{"--unsafe-allow-unredacted"})
	if len(issues) != 1 || issues[0].Severity != StatusError {
		t.Errorf("expected 1 error issue for --unsafe-allow-unredacted, got: %v", issues)
	}

	// 2. Allow network flag produces warning
	issues = engine.CheckUnsafeFlags([]string{"--allow-network"})
	if len(issues) != 1 || issues[0].Severity != StatusWarning {
		t.Errorf("expected 1 warning issue for --allow-network, got: %v", issues)
	}

	// 3. Safe flags produce no issues
	issues = engine.CheckUnsafeFlags([]string{"--jobs=4", "--profile=full"})
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for safe flags, got: %v", issues)
	}
}

func TestDoctor_RunAllChecks(t *testing.T) {
	tempDir := t.TempDir()
	gitignorePath := filepath.Join(tempDir, ".gitignore")
	_ = os.WriteFile(gitignorePath, []byte(".go-anything/local/\n*.capture\n"), 0644)

	engine := NewDoctorEngine(tempDir)
	report := engine.RunAllChecks([]string{"--jobs=4"})

	if !report.Healthy {
		t.Errorf("expected report to be healthy for clean repo, got issues: %v", report.Issues)
	}

	// Add leaked secret -> report becomes unhealthy
	secretFile := filepath.Join(tempDir, "token.env")
	_ = os.WriteFile(secretFile, []byte("GITHUB_TOKEN=ghp_1234567890abcdef1234567890abcdef1234\n"), 0644)

	reportUnhealthy := engine.RunAllChecks()
	if reportUnhealthy.Healthy {
		t.Errorf("expected report to be unhealthy when secrets are present")
	}
}
