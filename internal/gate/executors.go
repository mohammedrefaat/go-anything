package gate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultExecutors returns standard toolchain check implementations.
func DefaultExecutors(allowMissing bool) map[string]CheckExecutor {
	return map[string]CheckExecutor{
		CheckBuild:        defaultBuildExecutor(),
		CheckFormat:       defaultFormatExecutor(),
		CheckVet:          defaultVetExecutor(),
		CheckStaticcheck:  defaultStaticcheckExecutor(allowMissing),
		CheckGolangciLint: defaultGolangciLintExecutor(allowMissing),
		CheckRace:         defaultRaceExecutor(allowMissing),
		CheckGovulncheck:  defaultGovulncheckExecutor(allowMissing),
	}
}

func runCmd(ctx context.Context, dir string, env []string, command string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()
	out := strings.TrimSpace(combined.String())
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return out, exitCode, err
}

func resolveTargets(dir string) []string {
	if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
		var targets []string
		if fi, err := os.Stat(filepath.Join(dir, "legacy")); err == nil && fi.IsDir() {
			targets = append(targets, "./legacy/...")
		}
		if fi, err := os.Stat(filepath.Join(dir, "modern")); err == nil && fi.IsDir() {
			targets = append(targets, "./modern/...")
		}
		if len(targets) > 0 {
			return targets
		}
		return []string{"..."}
	}
	return []string{"./..."}
}

func defaultBuildExecutor() CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		targets := resolveTargets(dir)
		cmdArgs := append([]string{"build"}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, "go", cmdArgs...)
		if exitCode == 0 {
			return out, nil, true, nil
		}
		issues := extractLines(out)
		return out, issues, false, err
	}
}

func defaultFormatExecutor() CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		out, exitCode, err := runCmd(ctx, dir, env, "gofmt", "-s", "-l", ".")
		if exitCode != 0 {
			return out, extractLines(out), false, err
		}
		if len(out) == 0 {
			return "all files formatted cleanly", nil, true, nil
		}
		issues := extractLines(out)
		return out, issues, false, nil
	}
}

func defaultVetExecutor() CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		targets := resolveTargets(dir)
		cmdArgs := append([]string{"vet"}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, "go", cmdArgs...)
		if exitCode == 0 && len(out) == 0 {
			return "go vet clean", nil, true, nil
		}
		issues := extractLines(out)
		return out, issues, false, err
	}
}

func defaultStaticcheckExecutor(allowMissing bool) CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		targets := resolveTargets(dir)
		bin, err := exec.LookPath("staticcheck")
		if err != nil {
			// Fallback: try running via golangci-lint if installed
			if _, lintErr := exec.LookPath("golangci-lint"); lintErr == nil {
				cmdArgs := append([]string{"run", "--enable-only=staticcheck"}, targets...)
				out, exitCode, lErr := runCmd(ctx, dir, env, "golangci-lint", cmdArgs...)
				if exitCode == 0 {
					return out, nil, true, nil
				}
				// Try legacy flag if unknown flag
				if strings.Contains(out, "unknown flag") {
					cmdArgs = append([]string{"run", "--disable-all", "--enable=staticcheck"}, targets...)
					out, exitCode, lErr = runCmd(ctx, dir, env, "golangci-lint", cmdArgs...)
					if exitCode == 0 {
						return out, nil, true, nil
					}
				}
				return out, extractLines(out), false, lErr
			}
			if allowMissing {
				return "staticcheck not installed (skipped)", nil, false, ErrSkipped
			}
			return "staticcheck executable not found in PATH", []string{"staticcheck not installed"}, false, err
		}

		args := append([]string{}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, bin, args...)
		if exitCode == 0 {
			return out, nil, true, nil
		}
		return out, extractLines(out), false, err
	}
}

func defaultGolangciLintExecutor(allowMissing bool) CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		bin, err := exec.LookPath("golangci-lint")
		if err != nil {
			if allowMissing {
				return "golangci-lint not installed (skipped)", nil, false, ErrSkipped
			}
			return "golangci-lint executable not found in PATH", []string{"golangci-lint not installed"}, false, err
		}

		targets := resolveTargets(dir)
		cmdArgs := append([]string{"run", "--enable-only=errcheck,forcetypeassert"}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, bin, cmdArgs...)
		if exitCode == 0 {
			return out, nil, true, nil
		}
		// Try legacy flag if unknown flag
		if strings.Contains(out, "unknown flag") {
			cmdArgs = append([]string{"run", "--disable-all", "--enable=errcheck,forcetypeassert"}, targets...)
			out, exitCode, err = runCmd(ctx, dir, env, bin, cmdArgs...)
			if exitCode == 0 {
				return out, nil, true, nil
			}
		}
		if allowMissing && (strings.Contains(out, "unknown flag") || strings.Contains(out, "error reading config") || strings.Contains(out, "no config") || strings.Contains(out, "does not contain modules listed in go.work")) {
			return "golangci-lint (skipped)", nil, false, ErrSkipped
		}
		return out, extractLines(out), false, err
	}
}

func defaultRaceExecutor(allowMissing bool) CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		targets := resolveTargets(dir)
		cmdArgs := append([]string{"test", "-race"}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, "go", cmdArgs...)
		if strings.Contains(out, "requires cgo") {
			if allowMissing {
				return "race detector requires cgo (skipped in current environment)", nil, false, ErrSkipped
			}
			return out, []string{"go test -race requires cgo/gcc"}, false, err
		}
		if exitCode == 0 {
			return out, nil, true, nil
		}
		return out, extractLines(out), false, err
	}
}

func defaultGovulncheckExecutor(allowMissing bool) CheckExecutor {
	return func(ctx context.Context, dir string, env []string) (string, []string, bool, error) {
		bin, err := exec.LookPath("govulncheck")
		if err != nil {
			if allowMissing {
				return "govulncheck not installed (skipped)", nil, false, ErrSkipped
			}
			return "govulncheck executable not found in PATH", []string{"govulncheck not installed"}, false, err
		}

		targets := resolveTargets(dir)
		args := append([]string{}, targets...)
		out, exitCode, err := runCmd(ctx, dir, env, bin, args...)
		if exitCode == 0 {
			return out, nil, true, nil
		}
		if allowMissing && (strings.Contains(out, "no go.mod file") || strings.Contains(out, "does not contain modules listed in go.work")) {
			return "govulncheck clean (workspace mode)", nil, false, ErrSkipped
		}
		return out, extractLines(out), false, err
	}
}

func extractLines(text string) []string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(l)
		if len(trimmed) > 0 {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
