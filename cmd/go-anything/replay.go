package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cliadapter "github.com/mohammedrefaat/go-anything/adapters/application/cli"
	httpadapter "github.com/mohammedrefaat/go-anything/adapters/application/http"
	"github.com/mohammedrefaat/go-anything/internal/replay"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

func runReplay(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(stderr)

	unitID := fs.String("unit", "", "Migration unit identifier to replay (required)")
	fs.StringVar(unitID, "u", "", "Migration unit identifier (shorthand)")

	caseID := fs.String("case", "", "Filter and debug a single specific case ID")
	target := fs.String("target", "", "Target executable or HTTP base URL of ported Go service")
	fs.StringVar(target, "t", "", "Target executable or HTTP URL (shorthand)")

	modeStr := fs.String("mode", "", "Comparator mode (default: http_contract or structural_json)")
	fs.StringVar(modeStr, "m", "", "Comparator mode (shorthand)")

	jobs := fs.Int("jobs", global.Jobs, "Parallel replay workers")
	fs.IntVar(jobs, "j", global.Jobs, "Parallel replay workers (shorthand)")

	dirPath := fs.String("dir", ".", "Path to workspace repository root")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *unitID == "" {
		return fmt.Errorf("flag --unit (-u) is required")
	}

	absPath, err := filepath.Abs(*dirPath)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	store := state.NewStore(absPath)
	if _, err := store.GetManifest(); err != nil {
		return fmt.Errorf("workspace not found at %s: run 'go-anything init' first", absPath)
	}

	cases, err := loadCasesForUnit(absPath, *unitID)
	if err != nil {
		return err
	}

	if *caseID != "" {
		var filtered []*adapterkit.Case
		for _, c := range cases {
			if c.CaseID == *caseID {
				filtered = append(filtered, c)
				break
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("case ID %q not found in cases for unit %q", *caseID, *unitID)
		}
		cases = filtered
	}

	rules, _ := store.GetRules()

	// Build target driver
	driver, err := buildTargetReplayDriver(*target)
	if err != nil {
		return err
	}
	defer driver.Close()

	engineOpts := []replay.Option{
		replay.WithJobs(*jobs),
		replay.WithRules(rules),
	}
	if *modeStr != "" {
		engineOpts = append(engineOpts, replay.WithMode(*modeStr))
	}

	engine := replay.New(engineOpts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	report, err := engine.ReplayCases(ctx, *unitID, cases, driver)
	if err != nil {
		return fmt.Errorf("replay execution error: %w", err)
	}

	// Persist report under .go-anything/reports/
	reportJSON, err := report.ToJSON()
	if err == nil {
		reportFile := filepath.Join(store.ReportsDir(), fmt.Sprintf("rep-%s-replay.json", *unitID))
		_ = os.WriteFile(reportFile, reportJSON, 0644)
	}

	// Output report to stdout
	fmt.Fprintln(stdout, report.ToMarkdown())

	if report.FailedCases > 0 {
		var details strings.Builder
		for _, res := range report.Results {
			if !res.Passed {
				for _, m := range res.Mismatches {
					details.WriteString(fmt.Sprintf("\n  [%s] path=%s: exp=%v act=%v (%s)", res.CaseID, m.Path, m.Expected, m.Actual, m.Message))
				}
				if res.Error != "" {
					details.WriteString(fmt.Sprintf("\n  [%s] error: %s", res.CaseID, res.Error))
				}
			}
		}
		return fmt.Errorf("replay verification failed: %d/%d cases mismatched%s", report.FailedCases, report.TotalCases, details.String())
	}

	fmt.Fprintf(stdout, "PASS: 100%% replay matched (%d/%d cases)\n", report.PassedCases, report.TotalCases)
	return nil
}

func buildTargetReplayDriver(target string) (adapterkit.Replay, error) {
	if target == "" {
		return nil, fmt.Errorf("target ported implementation must be specified with --target")
	}

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return httpadapter.NewAdapter(httpadapter.WithBaseURL(target)), nil
	}

	// Subprocess CLI driver backed by cli.Adapter
	adapter := cliadapter.NewAdapter(target)
	return &cliReplayDriver{target: target, adapter: adapter}, nil
}

type cliReplayDriver struct {
	target  string
	adapter *cliadapter.Adapter
}

func (d *cliReplayDriver) DriverName() string {
	return d.adapter.DriverName()
}

func (d *cliReplayDriver) ExecuteCase(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	caseCopy := *c
	argv := c.Input.Argv
	if len(argv) > 0 {
		isCommand := false
		if argv[0] == d.target || filepath.Base(argv[0]) == filepath.Base(d.target) {
			isCommand = true
		} else if _, err := os.Stat(argv[0]); err == nil {
			isCommand = true
		} else if strings.ContainsAny(argv[0], `/\`) || strings.HasSuffix(strings.ToLower(argv[0]), ".exe") {
			isCommand = true
		} else if _, err := exec.LookPath(argv[0]); err == nil {
			isCommand = true
		}

		if isCommand {
			caseCopy.Input.Argv = argv[1:]
		}
	}
	return d.adapter.ExecuteCase(ctx, &caseCopy)
}

func (d *cliReplayDriver) Close() error {
	return d.adapter.Close()
}
