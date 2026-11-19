package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mohammedrefat/go-anything/internal/frontdoor"
	"github.com/mohammedrefat/go-anything/internal/state"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

func runStatus(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	targetDir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		targetDir = args[0]
	}

	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	manifest, err := store.GetManifest()
	if err != nil {
		return fmt.Errorf("no .go-anything workspace found at %s: run 'go-anything init' first", absPath)
	}

	units, err := store.ListUnits()
	if err != nil {
		return fmt.Errorf("failed to list units: %w", err)
	}

	// Sort units deterministically by wave, then by ID (Rule 2)
	sort.Slice(units, func(i, j int) bool {
		if units[i].Wave != units[j].Wave {
			return units[i].Wave < units[j].Wave
		}
		return units[i].ID < units[j].ID
	})

	fmt.Fprintf(stdout, "go-anything status\n")
	fmt.Fprintf(stdout, "Workspace: %s\n", store.RootDir())
	fmt.Fprintf(stdout, "Project:   %s (%s -> %s, schema: %s)\n", manifest.ProjectName, manifest.SourceLanguage, manifest.TargetLanguage, manifest.SchemaVersion)
	fmt.Fprintf(stdout, "Default Profile: %s | Default Jobs: %d\n\n", global.Profile, global.Jobs)

	if len(units) == 0 {
		fmt.Fprintf(stdout, "No migration units registered yet.\nRun 'go-anything analyze' to discover units and plan waves.\n")
		return nil
	}

	fmt.Fprintf(stdout, "Units (%d total):\n", len(units))

	w := tabwriter.NewWriter(stdout, 0, 4, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tWAVE\tSTATE\tPROFILE\tCLAIM / LEASE\tFALLBACKS")
	fmt.Fprintln(w, "--\t----\t-----\t-------\t-------------\t---------")

	counts := make(map[adapterkit.UnitState]int)
	now := time.Now().UTC()

	for _, u := range units {
		counts[u.State]++

		claimStr := "-"
		lease, err := store.GetLease(u.ID)
		if err == nil && lease != nil && lease.IsValid() {
			remaining := lease.ExpiresAt.Sub(now).Round(time.Minute)
			claimStr = fmt.Sprintf("Claimed by %s (expires in %s)", lease.Owner, remaining)
		} else if u.State == adapterkit.StateBlocked {
			if reason, ok := u.Metadata["blocked_reason"].(string); ok && reason != "" {
				claimStr = fmt.Sprintf("Blocked: %s", reason)
			}
		}

		profile := string(u.EvidenceProfile)
		if profile == "" {
			profile = global.Profile
		}

		fallbackCount := int64(u.FallbackCount)
		if fb, err := frontdoor.GetFallbackCount(store.WorkspaceRoot(), u.ID); err == nil && fb > fallbackCount {
			fallbackCount = fb
		}

		fallbackStr := fmt.Sprintf("%d", fallbackCount)
		if frontdoor.IsUnitKillSwitchActive(store.WorkspaceRoot(), u.ID) {
			fallbackStr += " [KILL-SWITCH]"
		}

		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
			u.ID,
			u.Wave,
			u.State,
			profile,
			claimStr,
			fallbackStr,
		)
	}
	_ = w.Flush()

	fmt.Fprintf(stdout, "\nSummary: %d Completed · %d Cutover · %d Approved · %d Shadow · %d Verified · %d In progress · %d Ready · %d Planned · %d Blocked\n",
		counts[adapterkit.StateCompleted],
		counts[adapterkit.StateCutover],
		counts[adapterkit.StateApproved],
		counts[adapterkit.StateShadow],
		counts[adapterkit.StateVerified],
		counts[adapterkit.StateInProgress],
		counts[adapterkit.StateReady],
		counts[adapterkit.StatePlanned],
		counts[adapterkit.StateBlocked],
	)

	return nil
}
