package lift

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// Engine orchestrates the lift process by safely executing all checks on an isolated copy
// before synchronizing changes back to the workspace and marking units Completed.
type Engine struct {
	opts Options
}

// NewEngine initializes a lift Engine with the provided Options.
func NewEngine(opts Options) *Engine {
	if opts.Profile == "" {
		opts.Profile = adapterkit.ProfileReplay
	}
	if len(opts.BuildCommand) == 0 {
		opts.BuildCommand = []string{"go", "build", "./..."}
	}
	if opts.Actor == "" {
		opts.Actor = "lift-engine"
	}
	return &Engine{opts: opts}
}

// Run executes the complete lift lifecycle:
// 1. Pre-flight checks on real workspace (fallback counts must be 0, commit-tied evidence must exist).
// 2. Copies workspace to an isolated temporary directory.
// 3. Re-runs verification profile on the copy.
// 4. Removes front door bridges and original application files on the copy.
// 5. Rebuilds the ported code in the copy to confirm clean compilation.
// 6. Synchronizes removals back to the workspace and transitions units to Completed.
// If any check fails, the workspace remains completely untouched.
func (e *Engine) Run(ctx context.Context) (*Result, error) {
	if e.opts.WorkspaceRoot == "" {
		return nil, fmt.Errorf("WorkspaceRoot cannot be empty")
	}

	wsRoot := filepath.Clean(e.opts.WorkspaceRoot)
	if _, err := os.Stat(wsRoot); err != nil {
		return nil, fmt.Errorf("invalid workspace root %s: %w", wsRoot, err)
	}

	store := state.NewStore(wsRoot)

	// Step 1: Pre-flight checks on the real workspace
	targetUnits, blockers, err := e.preflightChecks(ctx, wsRoot, store)
	if err != nil {
		return nil, err
	}
	if len(blockers) > 0 {
		return &Result{
			Success:  false,
			Blockers: blockers,
		}, ErrLiftBlocked
	}

	// Step 2: Create isolated temporary copy of the project
	tempDir, err := os.MkdirTemp("", "go-anything-lift-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary working copy: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Copy workspace files, omitting git history and transient local locks
	ignorePatterns := []string{".git", ".go-anything/local"}
	if err := CopyDirectory(wsRoot, tempDir, ignorePatterns); err != nil {
		return nil, fmt.Errorf("failed to copy workspace to %s: %w", tempDir, err)
	}

	// Step 3: Re-run verification profile in the copy
	if e.opts.ReplayRunner != nil {
		if err := e.opts.ReplayRunner(ctx, tempDir, targetUnits); err != nil {
			return &Result{
				Success:       false,
				Blockers:      []string{fmt.Sprintf("verification profile re-run failed in copy: %v", err)},
				CopyDirectory: tempDir,
			}, ErrLiftBlocked
		}
	}

	// Step 4: Remove front door bridges and original application files in the copy
	pathsToRemove := e.collectPathsToRemove(targetUnits)
	var removedFiles []string
	for _, relPath := range pathsToRemove {
		removed, err := RemovePathInDirectory(tempDir, relPath)
		if err != nil {
			return nil, fmt.Errorf("failed removing path %s in copy: %w", relPath, err)
		}
		removedFiles = append(removedFiles, removed...)
	}

	// Step 5: Execute build check in the copy
	if !e.opts.SkipBuild {
		if e.opts.BuildRunner != nil {
			out, err := e.opts.BuildRunner(ctx, tempDir)
			if err != nil {
				return &Result{
					Success:       false,
					Blockers:      []string{fmt.Sprintf("build failed in copy: %s: %v", out, err)},
					CopyDirectory: tempDir,
				}, ErrLiftBlocked
			}
		} else {
			cmd := exec.CommandContext(ctx, e.opts.BuildCommand[0], e.opts.BuildCommand[1:]...)
			cmd.Dir = tempDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				return &Result{
					Success:       false,
					Blockers:      []string{fmt.Sprintf("build failed in copy: %s: %v", string(out), err)},
					CopyDirectory: tempDir,
				}, ErrLiftBlocked
			}
		}
	}

	// Step 6: All checks passed on the copy. Synchronize changes back to real workspace
	var appliedRemovals []string
	for _, relPath := range pathsToRemove {
		removed, err := RemovePathInDirectory(wsRoot, relPath)
		if err != nil {
			return nil, fmt.Errorf("failed synchronizing removal of %s to workspace: %w", relPath, err)
		}
		appliedRemovals = append(appliedRemovals, removed...)
	}

	// Mark units Completed in store
	var completedUnits []string
	for _, unit := range targetUnits {
		if err := e.transitionUnitToCompleted(store, unit); err != nil {
			return nil, fmt.Errorf("failed to transition unit %s to Completed: %w", unit.ID, err)
		}
		completedUnits = append(completedUnits, unit.ID)
	}

	return &Result{
		Success:        true,
		CompletedUnits: completedUnits,
		RemovedFiles:   appliedRemovals,
	}, nil
}

// preflightChecks inspects fallback counts, commit-tied evidence, and unit states.
func (e *Engine) preflightChecks(ctx context.Context, wsRoot string, store *state.Store) ([]*adapterkit.Unit, []string, error) {
	var blockers []string

	units, err := store.ListUnits()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list units from store: %w", err)
	}
	if len(units) == 0 {
		return nil, []string{"no units found in workspace to lift"}, nil
	}

	// Filter target units if requested
	var targetUnits []*adapterkit.Unit
	if len(e.opts.TargetUnits) > 0 {
		targetMap := make(map[string]bool, len(e.opts.TargetUnits))
		for _, id := range e.opts.TargetUnits {
			targetMap[id] = true
		}
		for _, u := range units {
			if targetMap[u.ID] {
				targetUnits = append(targetUnits, u)
			}
		}
		if len(targetUnits) == 0 {
			return nil, []string{"none of the specified target units exist in workspace"}, nil
		}
	} else {
		targetUnits = units
	}

	// Detect git commit SHA if not specified
	commitSHA := e.opts.CommitSHA
	if commitSHA == "" {
		if detected, err := detectGitCommitSHA(ctx, wsRoot); err == nil {
			commitSHA = detected
		}
	}

	for _, unit := range targetUnits {
		// 1. Fallback counter check
		fbCount, err := frontdoor.GetFallbackCount(wsRoot, unit.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read fallback count for %s: %w", unit.ID, err)
		}
		if fbCount > 0 || unit.FallbackCount > 0 {
			blockers = append(blockers, fmt.Sprintf("unit %s has non-zero fallback count (durable: %d, unit: %d; must be 0)", unit.ID, fbCount, unit.FallbackCount))
		}

		// 2. Unit state check: cannot be in Planned, Recorded, InProgress, or Blocked
		switch unit.State {
		case adapterkit.StateVerified, adapterkit.StateShadow, adapterkit.StateApproved, adapterkit.StateCutover, adapterkit.StateCompleted:
			// Allowed state
		default:
			blockers = append(blockers, fmt.Sprintf("unit %s is in state %q (must be Verified, Approved, or Cutover)", unit.ID, unit.State))
		}

		// 3. Evidence profile check tied to commit
		profile := unit.EvidenceProfile
		if profile == "" {
			profile = e.opts.Profile
		}

		evList, err := store.ListEvidenceForUnit(unit.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to inspect evidence for %s: %w", unit.ID, err)
		}

		validEvidenceFound := false
		var evidenceIssues []string
		for _, ev := range evList {
			if commitSHA != "" && ev.CommitSHA != commitSHA {
				continue
			}
			ok, issue := isEvidenceValidForProfile(ev, profile)
			if ok {
				validEvidenceFound = true
				break
			} else {
				evidenceIssues = append(evidenceIssues, issue)
			}
		}

		if !validEvidenceFound {
			if commitSHA != "" {
				blockers = append(blockers, fmt.Sprintf("unit %s is missing valid evidence for commit %s (profile %s)", unit.ID, commitSHA, profile))
			} else if len(evidenceIssues) > 0 {
				blockers = append(blockers, fmt.Sprintf("unit %s evidence unsatisfied: %s", unit.ID, strings.Join(evidenceIssues, "; ")))
			} else {
				blockers = append(blockers, fmt.Sprintf("unit %s has no evidence recorded for profile %s", unit.ID, profile))
			}
		}
	}

	return targetUnits, blockers, nil
}

// collectPathsToRemove gathers files and directories of bridges and originals to remove.
func (e *Engine) collectPathsToRemove(units []*adapterkit.Unit) []string {
	seen := make(map[string]bool)
	var paths []string

	addPath := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, filepath.ToSlash(p))
		}
	}

	for _, p := range e.opts.BridgePaths {
		addPath(p)
	}
	for _, p := range e.opts.OriginalPaths {
		addPath(p)
	}

	for _, u := range units {
		if u.Metadata == nil {
			continue
		}
		if orig, ok := u.Metadata["original_path"].(string); ok {
			addPath(orig)
		}
		if orig, ok := u.Metadata["original_file"].(string); ok {
			addPath(orig)
		}
		if orig, ok := u.Metadata["legacy_path"].(string); ok {
			addPath(orig)
		}
		if bridge, ok := u.Metadata["bridge_path"].(string); ok {
			addPath(bridge)
		}
		if bridge, ok := u.Metadata["bridge_file"].(string); ok {
			addPath(bridge)
		}
		if files, ok := u.Metadata["legacy_files"].([]any); ok {
			for _, f := range files {
				if s, ok := f.(string); ok {
					addPath(s)
				}
			}
		}
	}

	return paths
}

// transitionUnitToCompleted transitions a unit through lifecycle states until Completed.
func (e *Engine) transitionUnitToCompleted(store *state.Store, unit *adapterkit.Unit) error {
	u, err := store.GetUnit(unit.ID)
	if err != nil {
		return err
	}

	if u.State == adapterkit.StateCompleted {
		return nil
	}

	// Advance Verified -> Approved (skipping shadow) if needed
	if u.State == adapterkit.StateVerified {
		u, err = store.Transition(u.ID, adapterkit.StateApproved, state.TransitionOptions{
			Actor:      e.opts.Actor,
			SkipShadow: true,
			Reason:     "lift automation",
		})
		if err != nil {
			return err
		}
	}

	// Advance Shadow -> Approved if needed
	if u.State == adapterkit.StateShadow {
		u, err = store.Transition(u.ID, adapterkit.StateApproved, state.TransitionOptions{
			Actor:  e.opts.Actor,
			Reason: "lift automation",
		})
		if err != nil {
			return err
		}
	}

	// Advance Approved -> Cutover if needed
	if u.State == adapterkit.StateApproved {
		u, err = store.Transition(u.ID, adapterkit.StateCutover, state.TransitionOptions{
			Actor: e.opts.Actor,
		})
		if err != nil {
			return err
		}
	}

	// Advance Cutover -> Completed
	if u.State == adapterkit.StateCutover {
		u.FallbackCount = 0
		_ = store.SaveUnit(u)
		_, err = store.Transition(u.ID, adapterkit.StateCompleted, state.TransitionOptions{
			Actor: e.opts.Actor,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// isEvidenceValidForProfile checks whether an evidence record satisfies the specified profile.
func isEvidenceValidForProfile(ev *adapterkit.Evidence, profile adapterkit.EvidenceProfile) (bool, string) {
	if ev.MismatchesCount != 0 {
		return false, fmt.Sprintf("mismatches count is %d (must be 0)", ev.MismatchesCount)
	}

	checksMap := make(map[string]adapterkit.ReportStatus, len(ev.Checks))
	for _, c := range ev.Checks {
		checksMap[strings.ToLower(c.Name)] = c.Status
	}

	switch profile {
	case adapterkit.ProfileReplay:
		status, ok := checksMap["replay"]
		if !ok || status != adapterkit.StatusPass {
			return false, "profile 'replay' requires check 'replay' with pass status"
		}
	case adapterkit.ProfileFull:
		requiredChecks := []string{"build", "format", "vet", "race", "replay", "error_paths", "api_compat"}
		for _, req := range requiredChecks {
			status, ok := checksMap[req]
			if !ok {
				return false, fmt.Sprintf("profile 'full' requires check %q", req)
			}
			if status != adapterkit.StatusPass {
				return false, fmt.Sprintf("profile 'full' check %q has status %q (must be pass)", req, status)
			}
		}

		vulnStatus, hasVuln := checksMap["govulncheck"]
		if !hasVuln {
			vulnStatus, hasVuln = checksMap["vulncheck"]
		}
		if !hasVuln {
			return false, "profile 'full' requires vulnerability check ('govulncheck' or 'vulncheck')"
		}
		if vulnStatus != adapterkit.StatusPass {
			return false, fmt.Sprintf("profile 'full' vulnerability check has status %q (must be pass)", vulnStatus)
		}

		for _, c := range ev.Checks {
			if c.Status == adapterkit.StatusFail {
				return false, fmt.Sprintf("gate check %q failed", c.Name)
			}
		}
	default:
		status, ok := checksMap["replay"]
		if !ok || status != adapterkit.StatusPass {
			return false, fmt.Sprintf("profile %q requires check 'replay' with pass status", profile)
		}
	}

	return true, ""
}

// detectGitCommitSHA runs git rev-parse HEAD in the given directory.
func detectGitCommitSHA(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) == 40 {
		return sha, nil
	}
	return "", fmt.Errorf("invalid commit sha: %s", sha)
}
