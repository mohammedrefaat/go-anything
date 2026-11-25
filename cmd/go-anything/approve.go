package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

func runApprove(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitIDFlag := fs.String("unit", "", "Migration unit identifier to approve")
	fs.StringVar(unitIDFlag, "u", "", "Migration unit identifier (shorthand)")

	skipShadowFlag := fs.Bool("skip-shadow", false, "Explicitly skip shadow mode (required until shadow engine is enabled)")
	actorFlag := fs.String("actor", "human", "Approving human engineer or actor identity")
	dirFlag := fs.String("dir", ".", "Path to workspace repository root")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	unitID := *unitIDFlag
	if unitID == "" && fs.NArg() > 0 {
		unitID = fs.Arg(0)
	}
	if unitID == "" {
		return fmt.Errorf("unit ID is required (e.g. 'go-anything approve <unit>')")
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

	// Verify preconditions: unit must be Verified (or Shadow)
	if unit.State != adapterkit.StateVerified && unit.State != adapterkit.StateShadow {
		return fmt.Errorf("cannot approve unit %q: must be in 'Verified' or 'Shadow' state (currently %q)", unitID, unit.State)
	}

	// If unit is in Verified, skipping Shadow requires --skip-shadow
	if unit.State == adapterkit.StateVerified && !*skipShadowFlag {
		return fmt.Errorf("cannot approve unit %q from Verified state without shadow evidence: use --skip-shadow to bypass shadow mode", unitID)
	}

	// Record metadata
	if unit.Metadata == nil {
		unit.Metadata = make(map[string]any)
	}
	unit.Metadata["approved_by"] = actor
	unit.Metadata["approved_at"] = time.Now().UTC().Format(time.RFC3339)
	unit.Metadata["shadow_skipped"] = *skipShadowFlag
	_ = store.SaveUnit(unit)

	approvedUnit, err := store.Transition(unitID, adapterkit.StateApproved, state.TransitionOptions{
		Actor:      actor,
		SkipShadow: *skipShadowFlag,
	})
	if err != nil {
		return fmt.Errorf("approval transition failed: %w", err)
	}

	fmt.Fprintf(stdout, "Approved unit %q by %s.\n", unitID, actor)
	if *skipShadowFlag {
		fmt.Fprintf(stdout, "Shadow mode bypassed (--skip-shadow recorded in evidence).\n")
	}
	fmt.Fprintf(stdout, "State advanced: %s -> %s\n", unit.State, approvedUnit.State)
	return nil
}
