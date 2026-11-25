package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/mcp/sanitize"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectUnit_MultiLanguageAndSanitization(t *testing.T) {
	tests := []struct {
		name           string
		skipOnWindows  bool
		sourceLanguage string
		unitID         string
		files          map[string]string
		wantContains   []string
		wantAbsent     []string
	}{
		{
			name:           "python_unit_lists_py_files_without_content",
			sourceLanguage: "python",
			unitID:         "billing",
			files: map[string]string{
				"legacy/billing/app.py":          "SECRET_MARKER_DO_NOT_LEAK\ndef charge():\n    return True\n",
				"legacy/billing/models/types.py": "CLASS_BODY_SHOULD_NOT_APPEAR\n",
				"legacy/billing/README.txt":      "ignored non-adapter file\n",
			},
			wantContains: []string{
				"=== Unit Inspection: billing ===",
				"Languages: python",
				"- legacy/billing/app.py (",
				"- legacy/billing/models/types.py (",
				sanitize.DefaultStartTag,
				sanitize.DefaultEndTag,
			},
			wantAbsent: []string{
				"SECRET_MARKER_DO_NOT_LEAK",
				"CLASS_BODY_SHOULD_NOT_APPEAR",
				"README.txt",
			},
		},
		{
			name:           "mixed_python_and_ts_unit_lists_all_registered_adapter_languages",
			sourceLanguage: "python",
			unitID:         "gateway",
			files: map[string]string{
				"legacy/gateway/handler.py": "def handle(): pass\n",
				"legacy/gateway/client.ts":  "export const ok = true;\n",
				"legacy/gateway/legacy.go":  "package gateway\n",
			},
			wantContains: []string{
				"Languages: go, python, typescript",
				"- legacy/gateway/client.ts (",
				"- legacy/gateway/handler.py (",
				"- legacy/gateway/legacy.go (",
			},
			wantAbsent: []string{
				"export const ok",
				"def handle()",
			},
		},
		{
			name:           "python_unit_caps_file_listing",
			sourceLanguage: "python",
			unitID:         "largepkg",
			files: func() map[string]string {
				m := make(map[string]string, maxInspectSourceFiles+10)
				for i := 0; i < maxInspectSourceFiles+10; i++ {
					m[fmt.Sprintf("legacy/largepkg/mod_%03d.py", i)] = fmt.Sprintf("VALUE_%03d = %d\n", i, i)
				}
				return m
			}(),
			wantContains: []string{
				"Languages: python",
				"- legacy/largepkg/mod_000.py (",
				fmt.Sprintf("- legacy/largepkg/mod_%03d.py (", maxInspectSourceFiles-1),
				fmt.Sprintf("... and 10 more source files (showing first %d of %d)", maxInspectSourceFiles, maxInspectSourceFiles+10),
				sanitize.DefaultStartTag,
				sanitize.DefaultEndTag,
			},
			wantAbsent: []string{
				fmt.Sprintf("mod_%03d.py", maxInspectSourceFiles),
				"VALUE_000",
			},
		},
		{
			name:           "python_unit_sanitizes_bidi_override_in_filename",
			sourceLanguage: "python",
			unitID:         "bidi",
			files: map[string]string{
				"legacy/bidi/safe_\u202eevil.py": "x = 1\n",
			},
			wantContains: []string{
				"Languages: python",
				"- legacy/bidi/safe_evil.py (",
				sanitize.DefaultStartTag,
				sanitize.DefaultEndTag,
			},
			wantAbsent: []string{
				"\u202e",
			},
		},
		{
			name:           "python_unit_sanitizes_ansi_and_boundary_spoof_in_filenames",
			skipOnWindows:  true,
			sourceLanguage: "python",
			unitID:         "untrusted",
			files: map[string]string{
				"legacy/untrusted/bad\x1b[31m_red.py":                            "x = 1\n",
				"legacy/untrusted/break_" + sanitize.DefaultEndTag + "_after.py": "y = 2\n",
			},
			wantContains: []string{
				"Languages: python",
				"- legacy/untrusted/bad_red.py (",
				"[END_UNTRUSTED_DATA_ESCAPED]",
				sanitize.DefaultStartTag,
				sanitize.DefaultEndTag,
			},
			wantAbsent: []string{
				"\x1b[31m",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("NTFS forbids control chars and '<'/'>' in filenames")
			}
			dir := t.TempDir()
			store, err := state.InitializeWorkspace(dir, "py-fixture", tc.sourceLanguage)
			require.NoError(t, err)

			require.NoError(t, store.SaveUnit(&adapterkit.Unit{
				ID:              tc.unitID,
				Name:            tc.unitID,
				State:           adapterkit.StatePlanned,
				EvidenceProfile: adapterkit.ProfileReplay,
			}))

			for rel, content := range tc.files {
				full := filepath.Join(dir, filepath.FromSlash(rel))
				require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
				require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
			}

			srv := NewServer()
			args, err := json.Marshal(map[string]string{
				"unit_id": tc.unitID,
				"dir":     dir,
			})
			require.NoError(t, err)

			res, err := srv.handleInspectUnit(context.Background(), args)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.False(t, res.IsError)
			require.Len(t, res.Content, 1)

			text := res.Content[0].Text
			for _, want := range tc.wantContains {
				assert.Contains(t, text, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, text, absent)
			}
			// Verify boundary tags wrap the full response.
			assert.True(t, strings.HasPrefix(text, sanitize.DefaultStartTag))
			assert.True(t, strings.HasSuffix(strings.TrimSpace(text), sanitize.DefaultEndTag))
		})
	}
}
