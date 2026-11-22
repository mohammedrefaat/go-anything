package main

import (
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
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
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

	switch subcommand {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "go-anything %s (profile: %s, jobs: %d)\n", Version, global.Profile, global.Jobs)
		return nil

	case "help", "--help", "-h":
		printUsage(stdout)
		return nil

	case "init":
		return runInit(global, subArgs, stdout, stderr)

	case "modernize":
		// Modernize is an alias for init --source=go
		return runModernize(global, subArgs, stdout, stderr)

	case "status":
		return runStatus(global, subArgs, stdout, stderr)

	case "doctor":
		return runDoctor(global, subArgs, stdout, stderr)

	case "record":
		return runRecord(global, subArgs, stdout, stderr)

	case "replay":
		return runReplay(global, subArgs, stdout, stderr)

	case "verify":
		return runVerify(global, subArgs, stdout, stderr)

	case "approve":
		return runApprove(global, subArgs, stdout, stderr)

	case "promote":
		return runPromote(global, subArgs, stdout, stderr)

	case "lift":
		return runLift(global, subArgs, stdout, stderr)

	case "migrate":
		return runMigrate(global, subArgs, stdout, stderr)

	case "template":
		return runTemplate(global, subArgs, stdout, stderr)

	case "setup":
		return runTemplate(global, subArgs, stdout, stderr)

	case "mcp":
		return runMCP(global, subArgs, stdin, stdout, stderr)

	case "analyze", "diff", "gate", "shadow", "cutover":
		fmt.Fprintf(stdout, "go-anything: command '%s' is registered and will be wired in milestone M1-M4.\n", subcommand)
		return nil

	default:
		return fmt.Errorf("unknown command %q. Run 'go-anything --help' for usage", subcommand)
	}
}

func parseGlobalFlags(args []string, global *GlobalFlags) ([]string, error) {
	var remaining []string
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--jobs" || arg == "-j":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s requires an integer argument", arg)
			}
			var j int
			if _, err := fmt.Sscanf(args[i+1], "%d", &j); err != nil || j <= 0 {
				return nil, fmt.Errorf("invalid value for --jobs: %s", args[i+1])
			}
			global.Jobs = j
			i += 2
		case len(arg) > 7 && arg[:7] == "--jobs=":
			var j int
			if _, err := fmt.Sscanf(arg[7:], "%d", &j); err != nil || j <= 0 {
				return nil, fmt.Errorf("invalid value for %s", arg)
			}
			global.Jobs = j
			i++
		case arg == "--profile" || arg == "-p":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s requires an argument", arg)
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
  gate        Execute Quality Gate pipeline
  shadow      Run front door in shadow mode on real traffic
  cutover     Switch primary traffic to verified Go unit
  lift        Remove fallback wrappers and finalize modernization
  migrate     Upgrade .go-anything/ workspace from v1alpha1 to stable v1 schemas
  template    Render agent instruction files (CLAUDE.md, .cursorrules, AGENTS.md, GEMINI.md)
  setup       Render and install agent instruction files into target workspace (alias for template)
  version     Show go-anything version
  help        Show command usage`)
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
