package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/internal/replay"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

var validCommitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func runVerify(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitIDFlag := fs.String("unit", "", "Migration unit identifier to verify")
	fs.StringVar(unitIDFlag, "u", "", "Migration unit identifier (shorthand)")

	profileFlag := fs.String("profile", "", "Verification evidence profile: 'replay' or 'full'")
	fs.StringVar(profileFlag, "p", "", "Verification evidence profile (shorthand)")

	targetFlag := fs.String("target", "", "Target executable or HTTP base URL of ported Go service")
	fs.StringVar(targetFlag, "t", "", "Target executable or HTTP URL (shorthand)")

	modeFlag := fs.String("mode", "", "Comparator mode (default: http_contract or structural_json)")
	fs.StringVar(modeFlag, "m", "", "Comparator mode (shorthand)")

	actorFlag := fs.String("actor", "agent", "Actor or agent running verification")
	dirFlag := fs.String("dir", ".", "Path to workspace repository root")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	unitID := *unitIDFlag
	if unitID == "" && fs.NArg() > 0 {
		unitID = fs.Arg(0)
	}
	if unitID == "" {
		return fmt.Errorf("unit ID is required (e.g. 'go-anything verify <unit>')")
	}

	absPath, err := filepath.Abs(*dirFlag)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	unit, err := store.GetUnit(unitID)
	if err != nil {
		return fmt.Errorf("unit %q not found in workspace: %w", unitID, err)
	}

	var profile adapterkit.EvidenceProfile
	if *profileFlag != "" {
		profile = adapterkit.EvidenceProfile(*profileFlag)
	} else if global != nil && global.ProfileExplicit && global.Profile != "" {
		profile = adapterkit.EvidenceProfile(global.Profile)
	} else if unit.EvidenceProfile != "" {
		profile = unit.EvidenceProfile
	} else if global != nil && global.Profile != "" {
		profile = adapterkit.EvidenceProfile(global.Profile)
	} else {
		profile = adapterkit.ProfileFull
	}

	// The full quality gate is scheduled for M4. Refuse verification before
	// executing the target or writing evidence until those checks exist.
	if profile == adapterkit.ProfileFull {
		return fmt.Errorf("full-profile verification is unavailable until the M4 quality gate is implemented")
	}

	// 1. Load recorded test cases
	cases, err := loadCasesForUnit(absPath, unitID)
	if err != nil {
		return fmt.Errorf("failed to load cases for unit %q: %w", unitID, err)
	}
	if len(cases) == 0 {
		return fmt.Errorf("no test cases recorded for unit %q: run 'go-anything record' first", unitID)
	}

	// 2. Resolve target ported driver
	target := *targetFlag
	if target == "" {
		// Check unit metadata for target or default ported binary location
		if metaTarget, ok := unit.Metadata["target"].(string); ok && metaTarget != "" {
			target = metaTarget
		}
	}
	if target == "" {
		return fmt.Errorf("target ported implementation must be specified with --target")
	}

	driver, err := buildTargetReplayDriver(target)
	if err != nil {
		return fmt.Errorf("failed to build replay driver for %s: %w", target, err)
	}
	defer driver.Close()

	// 3. Execute replay
	rules, _ := store.GetRules()
	engineOpts := []replay.Option{
		replay.WithJobs(global.Jobs),
		replay.WithRules(rules),
	}
	if *modeFlag != "" {
		engineOpts = append(engineOpts, replay.WithMode(*modeFlag))
	}

	engine := replay.New(engineOpts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	report, err := engine.ReplayCases(ctx, unitID, cases, driver)
	if err != nil {
		return fmt.Errorf("replay execution error: %w", err)
	}

	// Persist replay report
	if reportJSON, err := report.ToJSON(); err == nil {
		reportFile := filepath.Join(store.ReportsDir(), fmt.Sprintf("rep-%s-replay.json", unitID))
		_ = os.WriteFile(reportFile, reportJSON, 0644)
	}

	// Refuse if any mismatches occurred
	if report.FailedCases > 0 {
		return fmt.Errorf("verification refused: %d/%d cases mismatched\n%s", report.FailedCases, report.TotalCases, report.ToMarkdown())
	}

	// 4. Resolve Git commit SHA for evidence binding
	commitSHA := resolveCommitSHA(ctx, absPath)

	// 5. Construct Evidence
	nowStr := time.Now().UTC().Format(time.RFC3339)
	checks := []adapterkit.GateCheck{
		{
			Name:   "replay",
			Status: adapterkit.StatusPass,
			Output: fmt.Sprintf("100%% replay matched (%d/%d cases)", report.PassedCases, report.TotalCases),
		},
	}

	ev := &adapterkit.Evidence{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		EvidenceID:      fmt.Sprintf("evi-%s-%d", unitID, time.Now().UnixNano()),
		UnitID:          unitID,
		CommitSHA:       commitSHA,
		VerifiedAt:      nowStr,
		VerifiedBy:      *actorFlag,
		Profile:         profile,
		MismatchesCount: 0,
		Checks:          checks,
	}

	// 6. Advance unit state to Verified
	// If currently Recorded, advance to Ready if dependencies are met
	if unit.State == adapterkit.StateRecorded {
		_, err = store.Transition(unitID, adapterkit.StateReady, state.TransitionOptions{})
		if err != nil {
			return fmt.Errorf("failed to advance unit to Ready: %w", err)
		}
		unit, _ = store.GetUnit(unitID)
	}

	// If currently Ready, transition to InProgress under lease
	if unit.State == adapterkit.StateReady {
		_, err = store.Transition(unitID, adapterkit.StateInProgress, state.TransitionOptions{
			Actor:    *actorFlag,
			LeaseTTL: 1 * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("failed to claim unit for verification: %w", err)
		}
	}

	verifiedUnit, err := store.Transition(unitID, adapterkit.StateVerified, state.TransitionOptions{
		Actor:    *actorFlag,
		Evidence: ev,
	})
	if err != nil {
		return fmt.Errorf("failed to transition unit to Verified: %w", err)
	}

	fmt.Fprintf(stdout, "PASS: Unit %q successfully verified under profile %q.\n", unitID, profile)
	fmt.Fprintf(stdout, "Commit SHA: %s | Evidence ID: %s\n", commitSHA, ev.EvidenceID)
	fmt.Fprintf(stdout, "State advanced: %s -> %s\n", unit.State, verifiedUnit.State)
	return nil
}

func resolveCommitSHA(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		sha := strings.TrimSpace(out.String())
		if validCommitSHARegex.MatchString(sha) {
			return sha
		}
	}
	// Fallback to valid 40-character zero SHA if not in a git repository
	return "0000000000000000000000000000000000000000"
}
