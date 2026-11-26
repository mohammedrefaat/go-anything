package parity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

// SaveCases writes cases into a deterministic JSONL file at path.
func SaveCases(path string, cases []*adapterkit.Case) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	var buf bytes.Buffer
	for _, c := range cases {
		data, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("failed to marshal case %s: %w", c.CaseID, err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}

// AppendCase appends a single case to a JSONL file at path.
func AppendCase(path string, c *adapterkit.Case) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal case %s: %w", c.CaseID, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open case file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to append case to %s: %w", path, err)
	}

	return f.Sync()
}

// ReadCases parses a JSONL file and returns all valid adapterkit.Case entries.
func ReadCases(path string) ([]*adapterkit.Case, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open case file %s: %w", path, err)
	}
	defer file.Close()

	var cases []*adapterkit.Case
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var c adapterkit.Case
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("failed to parse case at line %d in %s: %w", lineNum, path, err)
		}
		cases = append(cases, &c)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading case file %s: %w", path, err)
	}

	return cases, nil
}
