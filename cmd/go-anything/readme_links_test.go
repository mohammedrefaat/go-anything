package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var linkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
var headerRegex = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

func slugify(header string) string {
	s := strings.ToLower(header)
	s = strings.TrimSpace(s)
	// Remove markdown formatting like ` or ** or links, &
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "[", "")
	s = strings.ReplaceAll(s, "]", "")
	s = strings.ReplaceAll(s, "&", "")

	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func TestREADME_LinksAndAnchors(t *testing.T) {
	readmePath := filepath.Join("..", "..", "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		readmePath = "README.md"
	}
	contentBytes, err := os.ReadFile(readmePath)
	require.NoError(t, err)
	content := string(contentBytes)

	// Collect all anchors/headings
	headings := headerRegex.FindAllStringSubmatch(content, -1)
	anchors := make(map[string]bool)
	for _, h := range headings {
		raw := h[1]
		slug := slugify(raw)
		anchors[slug] = true
	}

	matches := linkRegex.FindAllStringSubmatch(content, -1)
	require.NotEmpty(t, matches)

	repoRoot := filepath.Dir(readmePath)

	for _, m := range matches {
		target := m[2]
		linkText := m[1]

		// Skip external HTTP/HTTPS links
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}

		// Anchor link
		if strings.HasPrefix(target, "#") {
			anchor := strings.TrimPrefix(target, "#")
			assert.True(t, anchors[anchor], "Broken anchor %q for text %q in README.md", target, linkText)
			continue
		}

		// Local file link (strip anchor if any, e.g. file.md#section)
		filePath := target
		if idx := strings.Index(filePath, "#"); idx != -1 {
			filePath = filePath[:idx]
		}

		// Check file existence
		fullPath := filepath.Join(repoRoot, filepath.FromSlash(filePath))
		_, err := os.Stat(fullPath)
		assert.NoError(t, err, "Broken file link %q for text %q in README.md (resolved to %s)", target, linkText, fullPath)
	}
}
