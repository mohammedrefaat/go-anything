package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

var (
	// Version is populated during build or defaults to dev.
	Version = "dev"
)

// GlobalFlags holds global CLI configurations.
type GlobalFlags struct {
	Jobs            int
	Profile         string
	ProfileExplicit bool
}

func main() {
	if err := runCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		code := GetExitCode(err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(code)
	}
}

func runCLI(args []string, stdout, stderr io.Writer) error {
	return runCLIWithIO(os.Stdin, stdout, stderr, args)
}

func runCLIWithIO(stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	global := &GlobalFlags{
		Jobs:    runtime.NumCPU(),
		Profile: "full",
	}

	// Parse arguments and extract global flags
	cmdArgs, err := parseGlobalFlags(args, global)
	if err != nil {
		return err
	}

	if len(cmdArgs) == 0 {
		printUsage(stdout)
		return nil
	}

	subcommand := cmdArgs[0]
	subArgs := cmdArgs[1:]

	var cmdErr error
	switch subcommand {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "go-anything %s (profile: %s, jobs: %d)\n", Version, global.Profile, global.Jobs)
		return nil

	case "help", "--help", "-h":
		printUsage(stdout)
		return nil

	case "init":
		cmdErr = runInit(global, subArgs, stdout, stderr)

	case "modernize":
		// Modernize is an alias for init --source=go
		cmdErr = runModernize(global, subArgs, stdout, stderr)

	case "status":
		cmdErr = runStatus(global, subArgs, stdout, stderr)

	case "doctor":
		cmdErr = runDoctor(global, subArgs, stdout, stderr)

	case "record":
		cmdErr = runRecord(global, subArgs, stdout, stderr)

	case "replay":
		cmdErr = runReplay(global, subArgs, stdout, stderr)

	case "verify":
		cmdErr = runVerify(global, subArgs, stdout, stderr)

	case "approve":
		cmdErr = runApprove(global, subArgs, stdout, stderr)

	case "promote":
		cmdErr = runPromote(global, subArgs, stdout, stderr)

	case "lift":
		cmdErr = runLift(global, subArgs, stdout, stderr)

	case "migrate":
		cmdErr = runMigrate(global, subArgs, stdout, stderr)

	case "template":
		cmdErr = runTemplate(global, subArgs, stdout, stderr)

	case "setup":
		cmdErr = runTemplate(global, subArgs, stdout, stderr)

	case "mcp":
		cmdErr = runMCP(global, subArgs, stdin, stdout, stderr)

	case "analyze":
		cmdErr = runAnalyze(global, subArgs, stdout, stderr)

	case "diff":
		cmdErr = runDiff(global, subArgs, stdout, stderr)

	case "gate":
		cmdErr = runGate(global, subArgs, stdout, stderr)

	case "shadow":
		cmdErr = runShadow(global, subArgs, stdout, stderr)

	case "cutover":
		cmdErr = runCutover(global, subArgs, stdout, stderr)

	case "fuzz":
		cmdErr = runFuzz(global, subArgs, stdout, stderr)

	case "bench":
		cmdErr = runBench(global, subArgs, stdout, stderr)

	case "profile":
		cmdErr = runProfile(global, subArgs, stdout, stderr)

	case "bridge":
		cmdErr = runBridge(global, subArgs, stdout, stderr)

	case "go":
		cmdErr = runGo(global, subArgs, stdout, stderr)

	case "enable":
		cmdErr = runEnable(global, subArgs, stdout, stderr)

	case "disable":
		cmdErr = runDisable(global, subArgs, stdout, stderr)

	case "refresh":
		cmdErr = runRefresh(global, subArgs, stdout, stderr)

	case "clean":
		cmdErr = runClean(global, subArgs, stdout, stderr)

	case "report":
		cmdErr = runReport(global, subArgs, stdout, stderr)

	case "map":
		cmdErr = runMap(global, subArgs, stdout, stderr)

	default:
		cmdErr = exitErrorf(ExitCodeUsage, "unknown command %q. Run 'go-anything --help' for usage", subcommand)
	}

	if errors.Is(cmdErr, flag.ErrHelp) {
		return nil
	}
	return cmdErr
}

func parseGlobalFlags(args []string, global *GlobalFlags) ([]string, error) {
	var remaining []string
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--jobs" || arg == "-j":
			if i+1 >= len(args) {
				return nil, exitErrorf(ExitCodeUsage, "flag %s requires an integer argument", arg)
			}
			var j int
			if _, err := fmt.Sscanf(args[i+1], "%d", &j); err != nil || j <= 0 {
				return nil, exitErrorf(ExitCodeUsage, "invalid value for --jobs: %s", args[i+1])
			}
			global.Jobs = j
			i += 2
		case len(arg) > 7 && arg[:7] == "--jobs=":
			var j int
			if _, err := fmt.Sscanf(arg[7:], "%d", &j); err != nil || j <= 0 {
				return nil, exitErrorf(ExitCodeUsage, "invalid value for %s", arg)
			}
			global.Jobs = j
			i++
		case arg == "--profile" || arg == "-p":
			if i+1 >= len(args) {
				return nil, exitErrorf(ExitCodeUsage, "flag %s requires an argument", arg)
			}
			global.Profile = args[i+1]
			global.ProfileExplicit = true
			i += 2
		case len(arg) > 10 && arg[:10] == "--profile=":
			global.Profile = arg[10:]
			global.ProfileExplicit = true
			i++
		case len(arg) > 3 && arg[:3] == "-p=":
			global.Profile = arg[3:]
			global.ProfileExplicit = true
			i++
		default:
			remaining = append(remaining, arg)
			i++
		}
	}
	return remaining, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `go-anything - Rewrite your app in Go, one piece at a time.

Usage:
  go-anything [global flags] <command> [flags] [arguments]

Global Flags:
  --jobs, -j <N>      Number of parallel jobs (default: runtime.NumCPU())
  --profile, -p <P>   Evidence profile: 'replay' or 'full' (default: 'full')

Available Commands:
  init        Initialize .go-anything/ workspace layout in target directory
  modernize   Initialize Go-to-Go modernization workspace (alias for init --source=go)
  status      Show migration progress, units, waves, and active leases
  doctor      Diagnose workspace health, check .gitignore, and scan for leaked secrets
  analyze     Analyze legacy dependencies and generate topological migration waves
  record      Record baseline behavioral test cases in sandbox
  replay      Execute concurrent deterministic replay of recorded cases
  verify      Replay cases, store commit-tied evidence, and advance state to Verified
  approve     Record human sign-off and advance state to Approved (--skip-shadow)
  promote     Switch front door routing to Go, test kill switch, and advance state to Cutover
  diff        Replay cases against Go implementation and show structural diffs
  fuzz        Execute differential fuzzing against recorded cases and target
  bench       Execute latency and throughput benchmarks between original and Go
  profile     Profile legacy execution and rank candidate hot functions for migration
  bridge      Generate and manage hot-path strangler sidecar bridges
  go          Execute guided end-to-end hot-path porting pipeline
  enable      Enable Go strangler bridge routing for a unit or function
  disable     Disable Go strangler bridge routing for a unit or function
  refresh     Re-record behavioral cases and evaluate updated diffs
  clean       Remove generated bridge artifacts while preserving developer logic
  report      Generate comprehensive Markdown/JSON scorecard report for a unit
  map         List functions in a unit and display porting and routing status
  gate        Execute Quality Gate pipeline
  shadow      Run front door in shadow mode on real traffic
  cutover     Switch primary traffic to verified Go unit
  lift        Remove fallback wrappers and finalize modernization
  migrate     Upgrade .go-anything/ workspace from v1alpha1 to stable v1 schemas
  template    Render agent instruction files (CLAUDE.md, .cursorrules, AGENTS.md, GEMINI.md)
  setup       Render and install agent instruction files into target workspace (alias for template)
  version     Show go-anything version
  help        Show command usage

Exit Codes:
  0   Success / OK
  1   Failed verification check, gate failure, or diff mismatch
  2   Usage error (invalid flags, missing arguments)
  3   Internal error`)
}

// reorderArgs places flags and flag values before positional arguments so that
// standard flag.FlagSet can parse flags placed after positional arguments.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags []string
	var positionals []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			eqIdx := strings.Index(name, "=")
			if eqIdx != -1 {
				name = name[:eqIdx]
			}
			f := fs.Lookup(name)
			isBool := false
			if f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					isBool = true
				}
			}
			if eqIdx == -1 && !isBool && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positionals = append(positionals, arg)
		}
	}
	return append(flags, positionals...)
}
