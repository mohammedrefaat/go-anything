package mcp

import (
	"context"
	"encoding/json"
	"github.com/mohammedrefaat/go-anything/internal/state"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/require"
)

func TestHTTPPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	for _, target := range []string{server.URL, "http://10.0.0.1", "http://169.254.169.254", "http://[::1]", "file:///etc/passwd"} {
		_, err := createHTTPExecutionFunc(target)(context.Background(), adapterkit.CaseInput{})
		require.Error(t, err)
	}
	out, err := createHTTPExecutionFunc(server.URL, true)(context.Background(), adapterkit.CaseInput{})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Body)
	for _, size := range []int64{maxHTTPResponseBytes, maxHTTPResponseBytes + 1} {
		b := &boundedBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", int(size)))), remaining: maxHTTPResponseBytes}
		_, err := io.ReadAll(b)
		if size > maxHTTPResponseBytes {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer redirect.Close()
	_, err = createHTTPExecutionFunc(redirect.URL, true)(context.Background(), adapterkit.CaseInput{})
	require.Error(t, err)
}
func TestProfilesCannotBeReplaced(t *testing.T) {
	for _, declared := range []adapterkit.EvidenceProfile{"", adapterkit.ProfileFull, adapterkit.ProfileReplay, "invalid"} {
		for _, requested := range []string{"", "full", "replay", "invalid"} {
			effective := declared
			if effective == "" {
				effective = adapterkit.ProfileFull
			}
			_, err := validatedProfile(&adapterkit.Unit{EvidenceProfile: declared}, requested)
			valid := effective != "invalid" && (requested == "" || requested == string(effective))
			require.Equal(t, valid, err == nil)
		}
	}
}
func TestEvidenceRequiresCommitAndCleanTree(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	_, err := resolveCommitSHA(ctx, dir)
	require.Error(t, err)
	for _, args := range [][]string{{"init"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	_, err = resolveCommitSHA(ctx, dir)
	require.NoError(t, err)
	require.NoError(t, requireCleanTree(ctx, dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "source.go"), []byte("package test"), 0600))
	require.Error(t, requireCleanTree(ctx, dir))
}
func TestHandoffConfinedAndAtomic(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	s := NewServer()
	s.WorkspaceRoot = root
	victim := filepath.Join(outside, "victim")
	require.NoError(t, os.WriteFile(victim, []byte("preserve"), 0600))
	require.NoError(t, os.Symlink(victim, filepath.Join(root, "HANDOFF.md")))
	args, _ := json.Marshal(map[string]string{"summary": "summary", "next_steps": "next", "dir": root})
	_, err := s.handleHandoff(context.Background(), args)
	require.NoError(t, err)
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "preserve", string(data))
	args, _ = json.Marshal(map[string]string{"summary": "summary", "next_steps": "next", "dir": outside})
	_, err = s.handleHandoff(context.Background(), args)
	require.Error(t, err)
}
func TestLargeJSONLCase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cases.jsonl")
	require.NoError(t, os.WriteFile(file, []byte(`{"case_id":"`+strings.Repeat("x", 100000)+`"}`), 0600))
	cases, err := loadCasesFromJSONL(file)
	require.NoError(t, err)
	require.Len(t, cases, 1)
}

func TestMarkVerifiedRejectsPostEvidenceEdits(t *testing.T) {
	dir := t.TempDir()
	store, err := state.InitializeWorkspace(dir, "fixture", "go")
	require.NoError(t, err)
	require.NoError(t, store.SaveUnit(&adapterkit.Unit{ID: "unit", State: adapterkit.StateReady, EvidenceProfile: adapterkit.ProfileReplay}))
	_, err = store.Transition("unit", adapterkit.StateInProgress, state.TransitionOptions{Actor: "agent", LeaseTTL: time.Hour})
	require.NoError(t, err)
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	sha, err := resolveCommitSHA(context.Background(), dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveEvidence(&adapterkit.Evidence{EvidenceID: "evidence", UnitID: "unit", CommitSHA: sha, Profile: adapterkit.ProfileReplay, VerifiedAt: time.Now().UTC().Format(time.RFC3339), VerifiedBy: "agent", Checks: []adapterkit.GateCheck{{Name: "replay", Status: adapterkit.StatusPass}}}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "edited.go"), []byte("package edited"), 0600))
	s := NewServer()
	args, _ := json.Marshal(map[string]string{"unit_id": "unit", "dir": dir})
	_, err = s.handleMarkVerified(context.Background(), args)
	require.ErrorContains(t, err, "working tree must be clean")
	unit, err := store.GetUnit("unit")
	require.NoError(t, err)
	require.Equal(t, adapterkit.StateInProgress, unit.State)
	args, _ = json.Marshal(map[string]string{"unit_id": "unit", "dir": dir, "commit_sha": sha})
	result, err := s.handleMarkVerified(context.Background(), args)
	require.NoError(t, err)
	require.False(t, result.IsError)
}
