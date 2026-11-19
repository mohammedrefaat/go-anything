package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

func loadCasesForUnit(workspaceRoot, unitID string) ([]*adapterkit.Case, error) {
	casesPath := filepath.Join(workspaceRoot, ".go-anything", "cases", unitID+".jsonl")
	file, err := os.Open(casesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no recorded cases found for unit %q (file not found: %s)", unitID, casesPath)
		}
		return nil, err
	}
	defer file.Close()

	var cases []*adapterkit.Case
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c adapterkit.Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("malformed JSONL on line %d of %s: %w", lineNum, casesPath, err)
		}
		cases = append(cases, &c)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading case file %s: %w", casesPath, err)
	}

	return cases, nil
}
