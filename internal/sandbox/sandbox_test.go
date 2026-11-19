package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the multi-purpose subprocess helper for integration testing.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "no subcommand provided\n")
		os.Exit(2)
	}

	subcommand := args[0]
	switch subcommand {
	case "check_stripped_env":
		if v := os.Getenv("SECRET_AWS_KEY"); v != "" {
			fmt.Fprintf(os.Stderr, "secret was not stripped: %s\n", v)
			os.Exit(1)
		}
		if v := os.Getenv("HOST_SAFE_VAR"); v != "safe-value" {
			fmt.Fprintf(os.Stderr, "expected HOST_SAFE_VAR=safe-value, got %q\n", v)
			os.Exit(1)
		}
		if v := os.Getenv("CUSTOM_INJECTED"); v != "custom-val" {
			fmt.Fprintf(os.Stderr, "expected CUSTOM_INJECTED=custom-val, got %q\n", v)
			os.Exit(1)
		}
		os.Exit(0)

	case "outbound_http":
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
			Timeout: 2 * time.Second,
		}
		_, err := client.Get("http://1.1.1.1")
		if err != nil {
			fmt.Fprintf(os.Stdout, "OUTBOUND_CALL_FAILED: %v\n", err)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "outbound call succeeded unexpectedly\n")
		os.Exit(1)

	case "write_files":
		data, err := os.ReadFile("input.txt")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read input.txt: %v\n", err)
			os.Exit(1)
		}
		if string(data) != "initial content" {
			fmt.Fprintf(os.Stderr, "unexpected input.txt content: %q\n", string(data))
			os.Exit(1)
		}
		if err := os.WriteFile("input.txt", []byte("modified content"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write input.txt: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile("output.txt", []byte("new file content"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write output.txt: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)

	case "runaway":
		time.Sleep(10 * time.Second)
		os.Exit(0)

	case "exit_with_code":
		code := 0
		if len(args) > 1 {
			c, err := strconv.Atoi(args[1])
			if err == nil {
				code = c
			}
		}
		os.Exit(code)

	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", subcommand)
		os.Exit(2)
	}
}

func helperCommand(subcommand string, extraArgs ...string) (string, []string, map[string]string) {
	cmd := os.Args[0]
	args := []string{"-test.run=TestHelperProcess", "--", subcommand}
	args = append(args, extraArgs...)
	customEnv := map[string]string{
		"GO_WANT_HELPER_PROCESS": "1",
	}
	return cmd, args, customEnv
}

func TestFilterEnv_AllowlistAndStripping(t *testing.T) {
	r := &execRunner{}

	_ = os.Setenv("TEST_SANDBOX_SECRET", "super-secret-value")
	_ = os.Setenv("TEST_SANDBOX_ALLOWED", "allowed-value")
	defer func() {
		_ = os.Unsetenv("TEST_SANDBOX_SECRET")
		_ = os.Unsetenv("TEST_SANDBOX_ALLOWED")
	}()

	tests := []struct {
		name         string
		allowlist    []string
		customEnv    map[string]string
		allowNetwork bool
		isBestEffort bool
		wantAllowed  bool
		wantSecret   bool
		wantCustom   bool
		wantProxy    bool
	}{
		{
			name:         "strips unlisted secret, keeps allowlisted and custom",
			allowlist:    []string{"TEST_SANDBOX_ALLOWED"},
			customEnv:    map[string]string{"MY_CUSTOM_VAR": "custom123"},
			allowNetwork: false,
			isBestEffort: true,
			wantAllowed:  true,
			wantSecret:   false,
			wantCustom:   true,
			wantProxy:    true,
		},
		{
			name:         "empty allowlist strips all host vars except system essentials",
			allowlist:    []string{},
			customEnv:    map[string]string{"ISOLATED": "true"},
			allowNetwork: true,
			isBestEffort: false,
			wantAllowed:  false,
			wantSecret:   false,
			wantCustom:   true,
			wantProxy:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				AllowNetwork: tc.allowNetwork,
				EnvAllowlist: tc.allowlist,
				CustomEnv:    tc.customEnv,
			}
			env := r.buildEnv(cfg, tc.isBestEffort)
			envMap := make(map[string]string)
			for _, item := range env {
				parts := strings.SplitN(item, "=", 2)
				if len(parts) == 2 {
					envMap[parts[0]] = parts[1]
				}
			}

			if tc.wantAllowed {
				if _, ok := envMap["TEST_SANDBOX_ALLOWED"]; !ok {
					t.Errorf("expected TEST_SANDBOX_ALLOWED in env, but not found")
				}
			} else {
				if _, ok := envMap["TEST_SANDBOX_ALLOWED"]; ok {
					t.Errorf("did not expect TEST_SANDBOX_ALLOWED in env, but found")
				}
			}

			if tc.wantSecret {
				if _, ok := envMap["TEST_SANDBOX_SECRET"]; !ok {
					t.Errorf("expected TEST_SANDBOX_SECRET in env, but not found")
				}
			} else {
				if _, ok := envMap["TEST_SANDBOX_SECRET"]; ok {
					t.Errorf("secret variable TEST_SANDBOX_SECRET was NOT stripped from env")
				}
			}

			if tc.wantCustom {
				for k, v := range tc.customEnv {
					if envMap[k] != v {
						t.Errorf("expected custom env %s=%s, got %s", k, v, envMap[k])
					}
				}
			}

			if tc.wantProxy {
				proxyKeys := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}
				for _, pk := range proxyKeys {
					if envMap[pk] != DefaultInvalidProxySink {
						t.Errorf("expected proxy key %s=%s, got %s", pk, DefaultInvalidProxySink, envMap[pk])
					}
				}
			}
		})
	}
}

func TestCopyDir(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "target")

	// Create nested structure
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subDir: %v", err)
	}

	file1 := filepath.Join(srcDir, "root.txt")
	if err := os.WriteFile(file1, []byte("root file content"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}

	file2 := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(file2, []byte("nested file content"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	// Verify target structure
	got1, err := os.ReadFile(filepath.Join(dstDir, "root.txt"))
	if err != nil || string(got1) != "root file content" {
		t.Errorf("copied root.txt mismatch: %v, %s", err, string(got1))
	}

	got2, err := os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	if err != nil || string(got2) != "nested file content" {
		t.Errorf("copied nested.txt mismatch: %v, %s", err, string(got2))
	}
}

func TestRunner_OutboundNetworkFailsByDefault(t *testing.T) {
	runner := New()
	cmd, args, customEnv := helperCommand("outbound_http")

	cfg := Config{
		Command:      cmd,
		Args:         args,
		CustomEnv:    customEnv,
		AllowNetwork: false, // Default: fail-closed security
		EnvAllowlist: []string{"PATH", "SystemRoot", "TMP", "TEMP"},
		Timeout:      10 * time.Second,
	}

	res, err := runner.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected helper to exit 0 on network block, got %d. Stderr: %s", res.ExitCode, res.Stderr)
	}

	if !strings.Contains(res.Stdout, "OUTBOUND_CALL_FAILED") {
		t.Errorf("expected stdout to report OUTBOUND_CALL_FAILED, got: %s", res.Stdout)
	}

	// On best-effort platforms (Windows/Darwin or Linux fallback), check warning
	if res.IsolationMode == "best-effort" {
		if res.IsolationWarn != BestEffortWarning {
			t.Errorf("expected IsolationWarn %q, got %q", BestEffortWarning, res.IsolationWarn)
		}
		if !strings.Contains(res.Stderr, BestEffortWarning) {
			t.Errorf("expected stderr to contain BestEffortWarning, got: %s", res.Stderr)
		}
	}
}

func TestRunner_UnlistedEnvVarsStripped(t *testing.T) {
	_ = os.Setenv("SECRET_AWS_KEY", "sensitive-key-to-strip")
	_ = os.Setenv("HOST_SAFE_VAR", "safe-value")
	defer func() {
		_ = os.Unsetenv("SECRET_AWS_KEY")
		_ = os.Unsetenv("HOST_SAFE_VAR")
	}()

	runner := New()
	cmd, args, customEnv := helperCommand("check_stripped_env")
	customEnv["CUSTOM_INJECTED"] = "custom-val"

	cfg := Config{
		Command:      cmd,
		Args:         args,
		CustomEnv:    customEnv,
		EnvAllowlist: []string{"HOST_SAFE_VAR", "PATH", "SystemRoot", "TMP", "TEMP"},
		Timeout:      10 * time.Second,
	}

	res, err := runner.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0 indicating stripped secrets, got %d. Stderr: %s", res.ExitCode, res.Stderr)
	}
}

func TestRunner_FileWritesStayInTempWorkingCopy(t *testing.T) {
	origDir := t.TempDir()
	origFile := filepath.Join(origDir, "input.txt")
	if err := os.WriteFile(origFile, []byte("initial content"), 0644); err != nil {
		t.Fatalf("failed to create seed file: %v", err)
	}

	runner := New()
	cmd, args, customEnv := helperCommand("write_files")

	cfg := Config{
		Command:           cmd,
		Args:              args,
		WorkingDir:        origDir,
		TempWorkingCopy:   true,
		RetainWorkingCopy: true,
		CustomEnv:         customEnv,
		EnvAllowlist:      []string{"PATH", "SystemRoot", "TMP", "TEMP"},
		Timeout:           10 * time.Second,
	}

	res, err := runner.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. Stderr: %s", res.ExitCode, res.Stderr)
	}

	// 1. Verify parent workspace is pristine
	parentContent, err := os.ReadFile(origFile)
	if err != nil {
		t.Fatalf("failed to read original file: %v", err)
	}
	if string(parentContent) != "initial content" {
		t.Errorf("original file was mutated! Expected 'initial content', got %q", string(parentContent))
	}

	parentOutput := filepath.Join(origDir, "output.txt")
	if _, err := os.Stat(parentOutput); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output.txt was created in original directory, expected isolation!")
	}

	// 2. Verify temporary working copy has the modifications
	if res.WorkingCopyDir == "" {
		t.Fatalf("expected non-empty WorkingCopyDir")
	}

	tempInput := filepath.Join(res.WorkingCopyDir, "input.txt")
	tempContent, err := os.ReadFile(tempInput)
	if err != nil || string(tempContent) != "modified content" {
		t.Errorf("temp copy input.txt mismatch: %v, %q", err, string(tempContent))
	}

	tempOutput := filepath.Join(res.WorkingCopyDir, "output.txt")
	tempOutContent, err := os.ReadFile(tempOutput)
	if err != nil || string(tempOutContent) != "new file content" {
		t.Errorf("temp copy output.txt mismatch: %v, %q", err, string(tempOutContent))
	}

	// 3. Verify Cleanup removes temp copy
	if res.Cleanup != nil {
		if err := res.Cleanup(); err != nil {
			t.Errorf("cleanup failed: %v", err)
		}
		if _, err := os.Stat(res.WorkingCopyDir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("temp working copy still exists after cleanup!")
		}
	}
}

func TestRunner_TimeoutTerminatesRunaway(t *testing.T) {
	runner := New()
	cmd, args, customEnv := helperCommand("runaway")

	start := time.Now()
	cfg := Config{
		Command:      cmd,
		Args:         args,
		CustomEnv:    customEnv,
		EnvAllowlist: []string{"PATH", "SystemRoot", "TMP", "TEMP"},
		Timeout:      200 * time.Millisecond,
	}

	res, err := runner.Run(context.Background(), cfg)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}

	if res == nil || !res.TimedOut {
		t.Errorf("expected res.TimedOut to be true")
	}

	// Ensure process was terminated promptly and didn't hang for 10 seconds
	if elapsed > 3*time.Second {
		t.Errorf("execution took %v, expected termination within 3s", elapsed)
	}
}

func TestRunner_ExitCodes(t *testing.T) {
	runner := New()

	tests := []struct {
		name     string
		code     int
		wantCode int
	}{
		{name: "exit 0", code: 0, wantCode: 0},
		{name: "exit 42", code: 42, wantCode: 42},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args, customEnv := helperCommand("exit_with_code", strconv.Itoa(tc.code))
			cfg := Config{
				Command:      cmd,
				Args:         args,
				CustomEnv:    customEnv,
				EnvAllowlist: []string{"PATH", "SystemRoot", "TMP", "TEMP"},
				Timeout:      10 * time.Second,
			}

			res, err := runner.Run(context.Background(), cfg)
			if err != nil {
				t.Fatalf("unexpected runner error: %v", err)
			}
			if res.ExitCode != tc.wantCode {
				t.Errorf("expected exit code %d, got %d", tc.wantCode, res.ExitCode)
			}
		})
	}
}

func TestRunner_EmptyCommandValidation(t *testing.T) {
	runner := New()
	cfg := Config{
		Command: "",
	}
	_, err := runner.Run(context.Background(), cfg)
	if !errors.Is(err, ErrEmptyCommand) {
		t.Errorf("expected ErrEmptyCommand, got %v", err)
	}
}
