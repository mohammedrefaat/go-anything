// Package fromtests ingests behavioral cases captured during test runs,
// applies fail-closed in-memory redaction, deduplicates identical inputs,
// enforces max-case limits, and persists regression test cases.
package fromtests

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/internal/redactor"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// CapturedRecord represents an individual JSON line emitted by test observation hooks.
type CapturedRecord struct {
	UnitID     string  `json:"unit_id"`
	TestName   string  `json:"test_name"`
	FnName     string  `json:"fn_name,omitempty"`
	Input      any     `json:"input"`
	Output     any     `json:"output"`
	DurationMS float64 `json:"duration_ms,omitempty"`
	Timestamp  string  `json:"timestamp,omitempty"`
}

// IngestConfig configures the test case ingester.
type IngestConfig struct {
	UnitID         string
	CaptureDir     string
	WorkspaceRoot  string
	MaxCases       int // default 500
	SourceRevision string
	Runtime        string
	Redactor       *redactor.Redactor
}

// IngestResult summarizes the ingestion metrics and created cases.
type IngestResult struct {
	TotalIngested int
	Deduplicated  int
	Capped        int
	SavedCases    []*adapterkit.Case
	CasesFile     string
}

// Ingester coordinates reading test captures, redacting secrets, deduplicating, and persisting.
type Ingester struct {
	cfg IngestConfig
	red *redactor.Redactor
}

// NewIngester creates a new Ingester.
func NewIngester(cfg IngestConfig) *Ingester {
	if cfg.MaxCases <= 0 {
		cfg.MaxCases = 500
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = "."
	}
	red := cfg.Redactor
	if red == nil {
		red = redactor.New()
	}
	return &Ingester{
		cfg: cfg,
		red: red,
	}
}

// Ingest reads all captured test records, redacts secrets, deduplicates, and saves cases.
func (ing *Ingester) Ingest(ctx context.Context) (*IngestResult, error) {
	files, err := ing.discoverCaptureFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to discover capture files: %w", err)
	}

	seenInputs := make(map[string]bool)
	var rawRecords []CapturedRecord

	for _, file := range files {
		recs, err := ing.readRecordsFromFile(file)
		if err != nil {
			continue
		}
		for _, r := range recs {
			if r.UnitID != "" && ing.cfg.UnitID != "" && r.UnitID != ing.cfg.UnitID {
				continue
			}
			rawRecords = append(rawRecords, r)
		}
	}

	totalIngested := len(rawRecords)
	dedupCount := 0
	var finalCases []*adapterkit.Case

	revision := ing.cfg.SourceRevision
	if revision == "" {
		revision = "HEAD"
	}
	rt := ing.cfg.Runtime
	if rt == "" {
		rt = "test"
	}

	caseCounter := 0
	for _, rec := range rawRecords {
		// Fingerprint input for deduplication
		inputBytes, _ := json.Marshal(rec.Input)
		h := sha256.Sum256(inputBytes)
		fp := hex.EncodeToString(h[:])
		if seenInputs[fp] {
			dedupCount++
			continue
		}
		seenInputs[fp] = true

		caseCounter++
		c := ing.buildCase(rec, caseCounter, revision, rt)

		// Rule R-4: Strictly apply in-memory fail-closed redaction BEFORE writing to disk
		redactedCase, err := ing.red.RedactCase(c)
		if err != nil {
			// Fail-closed: drop case on unparseable/unsafe secrets
			continue
		}

		finalCases = append(finalCases, redactedCase)
	}

	// Enforce max-cases cap
	cappedCount := 0
	if len(finalCases) > ing.cfg.MaxCases {
		cappedCount = len(finalCases) - ing.cfg.MaxCases
		finalCases = finalCases[:ing.cfg.MaxCases]
	}

	// Persist to .go-anything/cases/<unit>.jsonl
	casesDir := filepath.Join(ing.cfg.WorkspaceRoot, ".go-anything", "cases")
	casesFile := filepath.Join(casesDir, filepath.FromSlash(ing.cfg.UnitID)+".jsonl")
	if err := os.MkdirAll(filepath.Dir(casesFile), 0755); err != nil {
		return nil, fmt.Errorf("failed to create cases directory: %w", err)
	}

	f, err := os.OpenFile(casesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open cases file %s: %w", casesFile, err)
	}
	defer f.Close()

	for _, c := range finalCases {
		data, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return nil, fmt.Errorf("failed to write case to disk: %w", err)
		}
	}

	return &IngestResult{
		TotalIngested: totalIngested,
		Deduplicated:  dedupCount,
		Capped:        cappedCount,
		SavedCases:    finalCases,
		CasesFile:     casesFile,
	}, nil
}

func (ing *Ingester) discoverCaptureFiles() ([]string, error) {
	captureDir := ing.cfg.CaptureDir
	if captureDir == "" {
		return nil, nil
	}

	fi, err := os.Stat(captureDir)
	if err != nil {
		return nil, nil
	}

	if !fi.IsDir() {
		return []string{captureDir}, nil
	}

	var files []string
	entries, err := os.ReadDir(captureDir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".json")) {
			files = append(files, filepath.Join(captureDir, e.Name()))
		}
	}
	return files, nil
}

func (ing *Ingester) readRecordsFromFile(filePath string) ([]CapturedRecord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var records []CapturedRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r CapturedRecord
		if err := json.Unmarshal([]byte(line), &r); err == nil {
			records = append(records, r)
		}
	}
	return records, scanner.Err()
}

func (ing *Ingester) buildCase(rec CapturedRecord, counter int, rev, rt string) *adapterkit.Case {
	unitID := rec.UnitID
	if unitID == "" {
		unitID = ing.cfg.UnitID
	}

	caseInput := adapterkit.CaseInput{}
	switch inp := rec.Input.(type) {
	case string:
		caseInput.Body = inp
	case map[string]any:
		caseInput.Body = inp
		if argv, ok := inp["argv"].([]any); ok {
			for _, a := range argv {
				caseInput.Argv = append(caseInput.Argv, fmt.Sprintf("%v", a))
			}
		}
		if headers, ok := inp["headers"].(map[string]any); ok {
			caseInput.Headers = make(map[string]string)
			for k, v := range headers {
				caseInput.Headers[k] = fmt.Sprintf("%v", v)
			}
		}
	default:
		caseInput.Body = inp
	}

	caseOutput := adapterkit.CaseOutput{}
	switch out := rec.Output.(type) {
	case string:
		caseOutput.Stdout = out
	case map[string]any:
		if stdout, ok := out["stdout"].(string); ok {
			caseOutput.Stdout = stdout
		}
		if stderr, ok := out["stderr"].(string); ok {
			caseOutput.Stderr = stderr
		}
		if code, ok := out["exit_code"].(float64); ok {
			caseOutput.ExitCode = int(code)
		}
		if body, ok := out["body"]; ok {
			caseOutput.Body = body
		} else if caseOutput.Stdout == "" {
			caseOutput.Body = out
		}
	default:
		caseOutput.Body = out
	}

	meta := map[string]any{
		"recorded_from": "test",
	}
	if rec.TestName != "" {
		meta["test_name"] = rec.TestName
	}
	if rec.FnName != "" {
		meta["fn_name"] = rec.FnName
	}

	return &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         fmt.Sprintf("test-%s-%04d", unitID, counter),
		UnitID:         unitID,
		Kind:           adapterkit.KindFromTest,
		SourceRevision: rev,
		Runtime:        rt,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input:          caseInput,
		ExpectedOutput: caseOutput,
		Metadata:       meta,
	}
}
