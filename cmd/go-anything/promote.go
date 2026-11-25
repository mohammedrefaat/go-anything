package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/frontdoor"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

func runPromote(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitIDFlag := fs.String("unit", "", "Migration unit identifier to promote")
	fs.StringVar(unitIDFlag, "u", "", "Migration unit identifier (shorthand)")

	actorFlag := fs.String("actor", "human", "Engineer or actor performing promotion")
	dirFlag := fs.String("dir", ".", "Path to workspace repository root")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	unitID := *unitIDFlag
	if unitID == "" && fs.NArg() > 0 {
		unitID = fs.Arg(0)
	}
	if unitID == "" {
		return fmt.Errorf("unit ID is required (e.g. 'go-anything promote <unit>')")
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

	actor := *actorFlag
	if actor == "" {
		actor = "human"
	}

	// Guard: Precondition requires unit to be in Approved state
	if unit.State != adapterkit.StateApproved {
		return fmt.Errorf("cannot promote unit %q: must be in 'Approved' state (currently %q)", unitID, unit.State)
	}

	// 1. Mandatory Kill Switch Test
	localDir := filepath.Join(absPath, ".go-anything", "local")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("failed to prepare local directory: %w", err)
	}

	testKillFile := filepath.Join(localDir, unitID+".kill")
	if err := os.WriteFile(testKillFile, []byte("kill-switch-test\n"), 0644); err != nil {
		return fmt.Errorf("failed to write test kill switch file: %w", err)
	}

	if !frontdoor.IsUnitKillSwitchActive(absPath, unitID) {
		_ = os.Remove(testKillFile)
		return fmt.Errorf("kill switch verification failed: active override not detected for %s", unitID)
	}

	// Clean up test kill file
	_ = os.Remove(testKillFile)

	if frontdoor.IsUnitKillSwitchActive(absPath, unitID) {
		return fmt.Errorf("kill switch verification failed: override still active after removal for %s", unitID)
	}

	// 2. Switch routing to Go implementation
	if unit.Metadata == nil {
		unit.Metadata = make(map[string]any)
	}
	unit.Metadata["routing"] = "go"
	unit.Metadata["promoted_at"] = time.Now().UTC().Format(time.RFC3339)
	unit.Metadata["promoted_by"] = actor
	_ = store.SaveUnit(unit)

	// 3. Advance state to Cutover
	cutoverUnit, err := store.Transition(unitID, adapterkit.StateCutover, state.TransitionOptions{
		Actor: actor,
	})
	if err != nil {
		return fmt.Errorf("promotion transition failed: %w", err)
	}

	fmt.Fprintf(stdout, "Promoted unit %q: routing switched to Go implementation.\n", unitID)
	fmt.Fprintf(stdout, "Kill switch tested and verified operational.\n")
	fmt.Fprintf(stdout, "State advanced: %s -> %s\n", unit.State, cutoverUnit.State)
	return nil
}
