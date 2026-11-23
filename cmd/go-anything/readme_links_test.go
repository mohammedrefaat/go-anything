package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var linkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
var htmlLinkRegex = regexp.MustCompile(`(?i)<a\s+[^>]*href=["']([^"']+)["'][^>]*>`)
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

func verifyDocLinks(t *testing.T, relPath string) {
	docPath := relPath
	if _, err := os.Stat(docPath); os.IsNotExist(err) {
		docPath = filepath.Join("..", "..", relPath)
	}
	contentBytes, err := os.ReadFile(docPath)
	require.NoError(t, err, "Failed to read doc file %s", docPath)
	content := string(contentBytes)

	// Collect all anchors/headings
	headings := headerRegex.FindAllStringSubmatch(content, -1)
	anchors := make(map[string]bool)
	for _, h := range headings {
		raw := h[1]
		slug := slugify(raw)
		anchors[slug] = true
	}

	repoRoot := filepath.Dir(docPath)
	if filepath.Base(filepath.Dir(docPath)) == "Docs" {
		repoRoot = filepath.Dir(filepath.Dir(docPath))
	} else if filepath.Base(docPath) == "README.md" {
		repoRoot = filepath.Dir(docPath)
	}

	type linkItem struct {
		text   string
		target string
	}
	var links []linkItem

	// Markdown links
	mdMatches := linkRegex.FindAllStringSubmatch(content, -1)
	for _, m := range mdMatches {
		links = append(links, linkItem{text: m[1], target: m[2]})
	}

	// HTML links (<a href="...">)
	htmlMatches := htmlLinkRegex.FindAllStringSubmatch(content, -1)
	for _, m := range htmlMatches {
		links = append(links, linkItem{text: "html-link", target: m[1]})
	}

	require.NotEmpty(t, links)

	for _, l := range links {
		target := l.target
		linkText := l.text

		// Skip external HTTP/HTTPS links
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}

		// Anchor link in same document
		if strings.HasPrefix(target, "#") {
			anchor := strings.TrimPrefix(target, "#")
			assert.True(t, anchors[anchor], "Broken anchor %q for text %q in %s", target, linkText, relPath)
			continue
		}

		// Local file link (strip anchor if any, e.g. file.md#section)
		filePath := target
		if idx := strings.Index(filePath, "#"); idx != -1 {
			filePath = filePath[:idx]
		}

		// URL unescape in case of %20, %26 etc.
		unescapedPath, err := url.PathUnescape(filePath)
		if err == nil {
			filePath = unescapedPath
		}

		// Check file existence relative to repoRoot
		fullPath := filepath.Join(repoRoot, filepath.FromSlash(filePath))
		if _, err := os.Stat(fullPath); err != nil {
			// Also try relative to the doc's directory
			altPath := filepath.Join(filepath.Dir(docPath), filepath.FromSlash(filePath))
			if _, errAlt := os.Stat(altPath); errAlt != nil {
				assert.NoError(t, err, "Broken file link %q for text %q in %s (resolved to %s)", target, linkText, relPath, fullPath)
			}
		}
	}
}

func TestREADME_LinksAndAnchors(t *testing.T) {
	verifyDocLinks(t, "README.md")
}

func TestInstallDoc_LinksAndAnchors(t *testing.T) {
	verifyDocLinks(t, filepath.Join("Docs", "install.md"))
}
