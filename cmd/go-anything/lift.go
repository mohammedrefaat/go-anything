package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/lift"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

func runLift(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("lift", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitIDFlag := fs.String("unit", "", "Specific migration unit to lift (default: lifts all ready units)")
	fs.StringVar(unitIDFlag, "u", "", "Migration unit identifier (shorthand)")

	profileFlag := fs.String("profile", "", "Evidence profile required for lift ('replay' or 'full')")
	fs.StringVar(profileFlag, "p", "", "Evidence profile (shorthand)")

	actorFlag := fs.String("actor", "human", "Engineer or actor performing lift")
	dirFlag := fs.String("dir", ".", "Path to workspace repository root")
	skipBuildFlag := fs.Bool("skip-build", false, "Skip the go build compilation check on the copy")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	unitID := *unitIDFlag
	if unitID == "" && fs.NArg() > 0 {
		unitID = fs.Arg(0)
	}

	absPath, err := filepath.Abs(*dirFlag)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	if _, err := store.GetManifest(); err != nil {
		return fmt.Errorf("workspace not found at %s: %w", absPath, err)
	}

	profile := adapterkit.ProfileReplay
	if *profileFlag != "" {
		profile = adapterkit.EvidenceProfile(*profileFlag)
	} else if global.Profile != "" {
		profile = adapterkit.EvidenceProfile(global.Profile)
	}

	var targetUnits []string
	if unitID != "" {
		targetUnits = []string{unitID}
	}

	actor := *actorFlag
	if actor == "" {
		actor = "human"
	}

	opts := lift.Options{
		WorkspaceRoot: absPath,
		TargetUnits:   targetUnits,
		Profile:       profile,
		Actor:         actor,
		SkipBuild:     *skipBuildFlag,
	}

	engine := lift.NewEngine(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	res, err := engine.Run(ctx)
	if err != nil || (res != nil && !res.Success) {
		if res != nil && len(res.Blockers) > 0 {
			fmt.Fprintf(stderr, "Lift operation blocked by %d condition(s):\n", len(res.Blockers))
			for _, b := range res.Blockers {
				fmt.Fprintf(stderr, " - %s\n", b)
			}
			return fmt.Errorf("lift blocked: %d condition(s) not met", len(res.Blockers))
		}
		return fmt.Errorf("lift operation failed: %w", err)
	}

	fmt.Fprintf(stdout, "SUCCESS: Lift complete!\n")
	fmt.Fprintf(stdout, "Completed units (%d): %v\n", len(res.CompletedUnits), res.CompletedUnits)
	if len(res.RemovedFiles) > 0 {
		fmt.Fprintf(stdout, "Removed bridge and original files (%d):\n", len(res.RemovedFiles))
		for _, f := range res.RemovedFiles {
			fmt.Fprintf(stdout, " - %s\n", f)
		}
	}
	fmt.Fprintf(stdout, "Workspace is fully modernized and verified in Go.\n")
	return nil
}
