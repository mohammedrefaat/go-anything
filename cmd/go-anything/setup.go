package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mohammedrefat/go-anything/internal/setup"
)

func runSetup(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	toolFlag := fs.String("tool", "all", "Agent tool to configure: claude-code, codex, cursor, gemini, opencode, or all")
	fs.StringVar(toolFlag, "t", "all", "Agent tool to configure (shorthand)")

	scopeFlag := fs.String("scope", "project", "Configuration scope: project or user")
	fs.StringVar(scopeFlag, "s", "project", "Configuration scope (shorthand)")

	dirFlag := fs.String("dir", ".", "Directory of the project workspace")
	fs.StringVar(dirFlag, "d", ".", "Directory of the project workspace (shorthand)")

	dryRunFlag := fs.Bool("dry-run", false, "Preview configuration changes without applying them")
	yesFlag := fs.Bool("yes", false, "Apply configuration changes directly without prompt")
	fs.BoolVar(yesFlag, "y", false, "Apply configuration changes directly (shorthand)")

	uninstallFlag := fs.Bool("uninstall", false, "Remove go-anything tool and MCP server registrations")
	jsonFlag := fs.Bool("json", false, "Output results as structured JSON")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	opts := setup.Options{
		Tool:      *toolFlag,
		Scope:     *scopeFlag,
		Dir:       *dirFlag,
		DryRun:    *dryRunFlag,
		Yes:       *yesFlag,
		Uninstall: *uninstallFlag,
	}

	// Default behavior: if neither --yes nor --dry-run is specified, run as dry-run preview
	if !opts.Yes && !opts.DryRun {
		opts.DryRun = true
	}

	res, err := setup.Run(opts)
	if err != nil {
		return err
	}

	if *jsonFlag {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	// Human-readable output
	banner := "=== go-anything agent setup ==="
	if res.Uninstall {
		banner = "=== go-anything agent uninstallation ==="
	}
	fmt.Fprintln(stdout, banner)
	fmt.Fprintf(stdout, "Scope:   %s\n", res.Scope)
	fmt.Fprintf(stdout, "Tool(s): %s\n", res.Tool)
	if res.DryRun {
		fmt.Fprintln(stdout, "Mode:    DRY-RUN (pass --yes to apply)")
	} else {
		fmt.Fprintln(stdout, "Mode:    APPLIED")
	}
	fmt.Fprintln(stdout)

	if len(res.Changes) == 0 {
		fmt.Fprintln(stdout, "No changes needed.")
		return nil
	}

	for _, ch := range res.Changes {
		actionTag := strings.ToUpper(ch.Action)
		fmt.Fprintf(stdout, "[%s] %s\n", actionTag, ch.Path)
		if ch.BackupPath != "" {
			fmt.Fprintf(stdout, "  Backup: %s\n", ch.BackupPath)
		}
		if ch.Instruction != "" {
			fmt.Fprintf(stdout, "  Instruction: %s\n", ch.Instruction)
		}
		if ch.Diff != "" {
			lines := strings.Split(strings.TrimSpace(ch.Diff), "\n")
			for _, line := range lines {
				fmt.Fprintf(stdout, "  %s\n", line)
			}
		}
	}

	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Summary: %s\n", res.Message)
	if res.DryRun {
		fmt.Fprintln(stdout, "To apply these changes, rerun with: go-anything setup --yes")
	}

	return nil
}
