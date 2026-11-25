package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

const GitignoreFileName = ".gitignore"
const LocalGitignoreEntry = ".go-anything/local/"

// InitializeWorkspace sets up the full .go-anything/ directory layout and ensures .gitignore coverage.
func InitializeWorkspace(workspaceRoot, projectName, srcLang string) (*Store, error) {
	store := NewStore(workspaceRoot)

	dirs := []string{
		store.RootDir(),
		store.UnitsDir(),
		store.LocalDir(),
		store.CasesDir(),
		store.ReportsDir(),
		store.EvidenceDir(),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	// Create initial manifest if not present
	if _, err := store.GetManifest(); err != nil {
		initialManifest := &adapterkit.Manifest{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			ProjectName:    projectName,
			SourceLanguage: srcLang,
			TargetLanguage: "go",
			Units:          []string{},
			Waves:          []adapterkit.Wave{},
		}
		if err := store.SaveManifest(initialManifest); err != nil {
			return nil, fmt.Errorf("failed to initialize manifest: %w", err)
		}
	}

	// Create initial rules.json if not present
	if _, err := store.GetRules(); err != nil {
		initialRules := &adapterkit.Rules{
			SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
			Wildcards:      []adapterkit.WildcardRule{},
			Normalizers:    []adapterkit.NormalizerRule{},
			FieldSemantics: []adapterkit.FieldSemantic{},
		}
		if err := store.SaveRules(initialRules); err != nil {
			return nil, fmt.Errorf("failed to initialize rules: %w", err)
		}
	}

	// Ensure .gitignore covers local/
	if err := EnsureGitignore(workspaceRoot); err != nil {
		return nil, fmt.Errorf("failed to ensure gitignore: %w", err)
	}

	return store, nil
}

// EnsureGitignore checks if .gitignore contains .go-anything/local/ and appends it if missing.
func EnsureGitignore(workspaceRoot string) error {
	gitignorePath := filepath.Join(workspaceRoot, GitignoreFileName)
	content, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	text := string(content)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == LocalGitignoreEntry || trimmed == ".go-anything/local" {
			return nil // already covered
		}
	}

	// Append entry
	entry := "\n# go-anything local agent leases and locks (Rule 1 & ADR-002)\n" + LocalGitignoreEntry + "\n"
	if len(text) == 0 {
		entry = strings.TrimPrefix(entry, "\n")
	}

	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("failed to append to .gitignore: %w", err)
	}

	return nil
}
