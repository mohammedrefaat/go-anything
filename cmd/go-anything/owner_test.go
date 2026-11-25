package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanonicalOwnerIdentity verifies that no occurrences of the legacy non-canonical
// owner handle (single 'a') exist across repository files, with an explicit
// allow-list restricted strictly to ADR-007 and DECISIONS.md.
func TestCanonicalOwnerIdentity(t *testing.T) {
	// Root of repository is two levels up from cmd/go-anything
	root, err := filepath.Abs("../..")
	require.NoError(t, err)

	legacyOwner := "mohammed" + "refat"

	isAllowListed := func(relPath string) bool {
		norm := filepath.ToSlash(relPath)
		base := filepath.Base(norm)
		if strings.EqualFold(base, "DECISIONS.md") {
			return true
		}
		if strings.EqualFold(base, "adr-007-canonical-owner-identity.md") {
			return true
		}
		return false
	}

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "Repository files have zero occurrences of legacy owner handle",
			run: func(t *testing.T) {
				var violations []string

				err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}

					rel, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					relSlash := filepath.ToSlash(rel)

					if info.IsDir() {
						name := info.Name()
						if name == ".git" || name == ".go-anything" || name == "node_modules" || name == "vendor" {
							return filepath.SkipDir
						}
						return nil
					}

					// Skip compiled binaries, archives, and build artifacts
					ext := strings.ToLower(filepath.Ext(info.Name()))
					if ext == ".exe" || ext == ".tar" || ext == ".gz" || ext == ".zip" || ext == ".sig" {
						return nil
					}

					if isAllowListed(relSlash) {
						return nil
					}

					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}

					// Look for legacy owner string (case-insensitive)
					// Note: Canonical "mohammedrefaat" has two 'a's and does not match legacyOwner.
					contentLower := strings.ToLower(string(data))
					if strings.Contains(contentLower, legacyOwner) {
						violations = append(violations, relSlash)
					}

					return nil
				})

				require.NoError(t, err)
				assert.Empty(t, violations, "Found %d file(s) containing non-canonical owner:\n%s",
					len(violations), strings.Join(violations, "\n"))
			},
		},
		{
			name: "go.mod declares canonical module path github.com/mohammedrefaat/go-anything",
			run: func(t *testing.T) {
				goModPath := filepath.Join(root, "go.mod")
				data, err := os.ReadFile(goModPath)
				require.NoError(t, err)

				lines := strings.Split(string(data), "\n")
				require.NotEmpty(t, lines)
				moduleLine := strings.TrimSpace(lines[0])
				assert.Equal(t, "module github.com/mohammedrefaat/go-anything", moduleLine)
			},
		},
		{
			name: "Allow-list strictly permits only ADR-007 and DECISIONS.md",
			run: func(t *testing.T) {
				assert.True(t, isAllowListed("DECISIONS.md"))
				assert.True(t, isAllowListed("Docs/decisions/adr-007-canonical-owner-identity.md"))
				assert.False(t, isAllowListed("README.md"))
				assert.False(t, isAllowListed("go.mod"))
				assert.False(t, isAllowListed("cmd/go-anything/main.go"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
