package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/skills"
	"github.com/mohammedrefat/go-anything/templates"
)

func runSetup(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	return runTemplateNamed("setup", global, args, stdout, stderr)
}

func runTemplate(global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	return runTemplateNamed("template", global, args, stdout, stderr)
}

func runTemplateNamed(cmdName string, global *GlobalFlags, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	agentFlag := fs.String("agent", "all", "Agent template to render: all, claude, cursor, codex, or gemini (comma-separated)")
	fs.StringVar(agentFlag, "a", "all", "Agent template to render (shorthand)")

	dirFlag := fs.String("dir", ".", "Workspace directory to write agent instruction files into")
	fs.StringVar(dirFlag, "d", ".", "Workspace directory (shorthand)")

	nameFlag := fs.String("name", "", "Project name used in rendered files (defaults to directory name)")
	forceFlag := fs.Bool("force", false, "Overwrite existing files")
	fs.BoolVar(forceFlag, "f", false, "Overwrite existing files (shorthand)")

	toStdout := fs.Bool("stdout", false, "Print rendered files to stdout instead of writing them")
	listFlag := fs.Bool("list", false, "List available agent templates and exit")
	withSkills := fs.Bool("skills", false, "Also write .go-anything/skills.md from the built-in skill definitions")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	registry := templates.DefaultRegistry()

	if *listFlag {
		fmt.Fprintf(stdout, "Available agent templates:\n")
		for _, name := range registry.Names() {
			t, _ := registry.Get(name)
			fmt.Fprintf(stdout, "  %-8s %-14s %s\n", name, t.FileName, t.Description)
		}
		return nil
	}

	targetDir := *dirFlag
	if fs.NArg() > 0 {
		targetDir = fs.Arg(0)
	}
	absPath, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path: %w", err)
	}

	projectName := *nameFlag
	if projectName == "" {
		projectName = filepath.Base(absPath)
	}

	vars := templates.Vars{
		ProjectName:    projectName,
		ModulePath:     detectModulePath(absPath),
		TargetLanguage: "go",
		Profile:        global.Profile,
	}

	selected, err := selectTemplates(registry, *agentFlag)
	if err != nil {
		return err
	}

	rendered := make([]templates.RenderedFile, 0, len(selected))
	for _, name := range selected {
		file, err := registry.Render(name, vars)
		if err != nil {
			return err
		}
		rendered = append(rendered, file)
	}

	if *toStdout {
		for i, file := range rendered {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			fmt.Fprintf(stdout, "=== %s ===\n", file.FileName)
			_, _ = stdout.Write(file.Content)
		}
		return nil
	}

	// Fail closed: never silently leave a stale instruction file in place.
	var conflicts []string
	for _, file := range rendered {
		path := filepath.Join(absPath, file.FileName)
		if _, statErr := os.Stat(path); statErr == nil && !*forceFlag {
			conflicts = append(conflicts, file.FileName)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("refusing to overwrite existing files (%s); pass --force to replace them",
			strings.Join(conflicts, ", "))
	}

	for _, file := range rendered {
		path := filepath.Join(absPath, file.FileName)
		if err := os.WriteFile(path, file.Content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		fmt.Fprintf(stdout, "rendered %-14s -> %s\n", file.Name, path)
	}

	if *withSkills {
		skillsDir := filepath.Join(absPath, ".go-anything")
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", skillsDir, err)
		}
		skillsPath := filepath.Join(skillsDir, "skills.md")
		if _, statErr := os.Stat(skillsPath); statErr == nil && !*forceFlag {
			return fmt.Errorf("refusing to overwrite existing file (%s); pass --force to replace it", skillsPath)
		}
		if err := os.WriteFile(skillsPath, []byte(skills.RenderAllMarkdown()), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", skillsPath, err)
		}
		fmt.Fprintf(stdout, "rendered %-14s -> %s\n", "skills", skillsPath)
	}

	fmt.Fprintf(stdout, "\nProject: %s (profile: %s)\n", vars.ProjectName, vars.Profile)
	return nil
}

func selectTemplates(registry *templates.Registry, spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "all") {
		return registry.Names(), nil
	}

	var selected []string
	seen := make(map[string]bool)
	for _, raw := range strings.Split(spec, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, ok := registry.Get(name); !ok {
			return nil, fmt.Errorf("unknown agent template %q (available: %s)",
				name, strings.Join(registry.Names(), ", "))
		}
		if !seen[name] {
			seen[name] = true
			selected = append(selected, name)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no agent templates selected")
	}
	return selected, nil
}

func detectModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
