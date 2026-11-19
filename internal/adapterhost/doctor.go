package adapterhost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/redactor"
)

// DoctorCheckStatus defines the severity status of a doctor check.
type DoctorCheckStatus string

const (
	StatusHealthy DoctorCheckStatus = "healthy"
	StatusWarning DoctorCheckStatus = "warning"
	StatusError   DoctorCheckStatus = "error"
)

// CheckIssue details a problem identified during health checks.
type CheckIssue struct {
	CheckName string            `json:"check_name"`
	Severity  DoctorCheckStatus `json:"severity"`
	Message   string            `json:"message"`
	Path      string            `json:"path,omitempty"`
}

// DoctorReport aggregates diagnostic results across all checks.
type DoctorReport struct {
	Healthy bool         `json:"healthy"`
	Issues  []CheckIssue `json:"issues"`
}

// DoctorEngine runs system and repository diagnostic checks.
type DoctorEngine struct {
	RepoPath string
}

// NewDoctorEngine returns a new DoctorEngine for the specified repository root.
func NewDoctorEngine(repoPath string) *DoctorEngine {
	if repoPath == "" {
		repoPath = "."
	}
	return &DoctorEngine{RepoPath: repoPath}
}

// CheckGitignore verifies that .gitignore covers sensitive local leases/locks (.go-anything/local/)
// and raw capture artifacts.
func (d *DoctorEngine) CheckGitignore() []CheckIssue {
	var issues []CheckIssue
	gitignorePath := filepath.Join(d.RepoPath, ".gitignore")

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			issues = append(issues, CheckIssue{
				CheckName: "gitignore",
				Severity:  StatusError,
				Message:   ".gitignore does not exist; '.go-anything/local/' and raw captures must be ignored",
				Path:      gitignorePath,
			})
			return issues
		}
		issues = append(issues, CheckIssue{
			CheckName: "gitignore",
			Severity:  StatusError,
			Message:   fmt.Sprintf("failed to read .gitignore: %v", err),
			Path:      gitignorePath,
		})
		return issues
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	hasLocal := false
	hasCaptures := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Match .go-anything/local/ or local/ patterns
		if strings.Contains(trimmed, ".go-anything/local") || trimmed == "local/" || trimmed == "local" || strings.Contains(trimmed, "local/*") {
			hasLocal = true
		}
		// Match capture artifacts
		if strings.Contains(trimmed, "captures") || strings.Contains(trimmed, "*.capture") || strings.Contains(trimmed, "capture") {
			hasCaptures = true
		}
	}

	if !hasLocal {
		issues = append(issues, CheckIssue{
			CheckName: "gitignore",
			Severity:  StatusError,
			Message:   "missing .gitignore rule: '.go-anything/local/' must be gitignored (Execution Rule 1)",
			Path:      gitignorePath,
		})
	}

	if !hasCaptures {
		issues = append(issues, CheckIssue{
			CheckName: "gitignore",
			Severity:  StatusWarning,
			Message:   "missing .gitignore rule: raw captures ('*.capture' or '.go-anything/captures/') should be gitignored",
			Path:      gitignorePath,
		})
	}

	return issues
}

// CheckCommittedSecrets scans repository files for unredacted sensitive tokens and credentials.
func (d *DoctorEngine) CheckCommittedSecrets() []CheckIssue {
	var issues []CheckIssue
	red := redactor.New()

	skipDirs := map[string]bool{
		".git":         true,
		"local":        true,
		"node_modules": true,
		"vendor":       true,
		".go-anything": false, // inspect .go-anything but skip .go-anything/local
	}

	err := filepath.WalkDir(d.RepoPath, func(path string, dEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := dEntry.Name()
		if dEntry.IsDir() {
			if skipDirs[name] {
				return filepath.SkipDir
			}
			// Skip .go-anything/local
			if strings.HasSuffix(filepath.ToSlash(path), ".go-anything/local") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip binary files and test binary artifacts
		if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".bin") {
			return nil
		}

		info, err := dEntry.Info()
		if err != nil || info.Size() > 1024*1024 { // skip files larger than 1MB
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		redacted, _, _ := red.RedactString(content)
		if redacted != content {
			relPath, _ := filepath.Rel(d.RepoPath, path)
			issues = append(issues, CheckIssue{
				CheckName: "committed_secrets",
				Severity:  StatusError,
				Message:   fmt.Sprintf("potential unredacted secret found in file: %s", relPath),
				Path:      path,
			})
		}
		return nil
	})

	if err != nil {
		issues = append(issues, CheckIssue{
			CheckName: "committed_secrets",
			Severity:  StatusError,
			Message:   fmt.Sprintf("failed to scan repository files: %v", err),
		})
	}

	return issues
}

// CheckUnsafeFlags verifies whether risky or security-bypassing flags are enabled.
func (d *DoctorEngine) CheckUnsafeFlags(flags []string) []CheckIssue {
	var issues []CheckIssue
	for _, f := range flags {
		switch f {
		case "--unsafe-allow-unredacted":
			issues = append(issues, CheckIssue{
				CheckName: "unsafe_flags",
				Severity:  StatusError,
				Message:   "flag '--unsafe-allow-unredacted' is set: fail-closed secret redaction is bypassed",
			})
		case "--allow-network":
			issues = append(issues, CheckIssue{
				CheckName: "unsafe_flags",
				Severity:  StatusWarning,
				Message:   "flag '--allow-network' is set: execution sandbox network isolation is disabled",
			})
		}
	}
	return issues
}

// RunAllChecks runs all diagnostic checks and compiles a DoctorReport.
func (d *DoctorEngine) RunAllChecks(flags ...[]string) *DoctorReport {
	report := &DoctorReport{Healthy: true}

	report.Issues = append(report.Issues, d.CheckGitignore()...)
	report.Issues = append(report.Issues, d.CheckCommittedSecrets()...)

	var activeFlags []string
	if len(flags) > 0 {
		activeFlags = flags[0]
	}
	report.Issues = append(report.Issues, d.CheckUnsafeFlags(activeFlags)...)

	for _, issue := range report.Issues {
		if issue.Severity == StatusError {
			report.Healthy = false
			break
		}
	}
	return report
}
