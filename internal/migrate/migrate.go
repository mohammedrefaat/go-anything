// Package migrate provides an idempotent migration engine to upgrade .go-anything
// workspaces from legacy schema versions (v1alpha1) to stable schemas (v1).
package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefat/go-anything/schemas"
)

// MigrationResult summarizes the files and counts upgraded during a workspace migration.
type MigrationResult struct {
	WorkspaceRoot      string   `json:"workspace_root"`
	DotDir             string   `json:"dot_dir"`
	FromVersion        string   `json:"from_version"`
	ToVersion          string   `json:"to_version"`
	ManifestUpgraded   bool     `json:"manifest_upgraded"`
	RulesUpgraded      bool     `json:"rules_upgraded"`
	UnitsTotal         int      `json:"units_total"`
	UnitsUpgraded      int      `json:"units_upgraded"`
	CasesFilesTotal    int      `json:"cases_files_total"`
	CasesCount         int      `json:"cases_count"`
	CasesUpgraded      int      `json:"cases_upgraded"`
	ReportsFilesTotal  int      `json:"reports_files_total"`
	ReportsUpgraded    int      `json:"reports_upgraded"`
	EvidenceFilesTotal int      `json:"evidence_files_total"`
	EvidenceUpgraded   int      `json:"evidence_upgraded"`
	TotalFilesModified int      `json:"total_files_modified"`
	UpgradedFiles      []string `json:"upgraded_files"`
}

// MigrateWorkspace upgrades a .go-anything workspace to v1 schemas idempotently.
// If workspacePath points to either the repository root or the .go-anything directory,
// it discovers all workspace files and upgrades v1alpha1 records to v1.
func MigrateWorkspace(workspacePath string) (*MigrationResult, error) {
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace path %s: %w", workspacePath, err)
	}

	var dotDir, workspaceRoot string
	if filepath.Base(absPath) == ".go-anything" {
		dotDir = absPath
		workspaceRoot = filepath.Dir(absPath)
	} else {
		dotDir = filepath.Join(absPath, ".go-anything")
		workspaceRoot = absPath
	}

	info, err := os.Stat(dotDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no .go-anything workspace found at %s", dotDir)
	}

	result := &MigrationResult{
		WorkspaceRoot: workspaceRoot,
		DotDir:        dotDir,
		FromVersion:   adapterkit.SchemaVersionV1Alpha1,
		ToVersion:     adapterkit.SchemaVersionV1,
		UpgradedFiles: make([]string, 0),
	}

	// 1. Migrate manifest.json
	manifestPath := filepath.Join(dotDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		upgraded, err := migrateJSONFile(manifestPath, "manifest.schema.json")
		if err != nil {
			return nil, fmt.Errorf("failed to migrate manifest.json: %w", err)
		}
		if upgraded {
			result.ManifestUpgraded = true
			result.TotalFilesModified++
			result.UpgradedFiles = append(result.UpgradedFiles, manifestPath)
		}
	} else if os.IsNotExist(err) {
		return nil, fmt.Errorf("manifest.json not found in %s", dotDir)
	} else {
		return nil, fmt.Errorf("error accessing manifest.json: %w", err)
	}

	// 2. Migrate rules.json (if present)
	rulesPath := filepath.Join(dotDir, "rules.json")
	if _, err := os.Stat(rulesPath); err == nil {
		upgraded, err := migrateJSONFile(rulesPath, "rules.schema.json")
		if err != nil {
			return nil, fmt.Errorf("failed to migrate rules.json: %w", err)
		}
		if upgraded {
			result.RulesUpgraded = true
			result.TotalFilesModified++
			result.UpgradedFiles = append(result.UpgradedFiles, rulesPath)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error accessing %s: %w", rulesPath, err)
	}

	// 3. Migrate units/ (*.json)
	unitsDir := filepath.Join(dotDir, "units")
	if info, err := os.Stat(unitsDir); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("expected units path %s to be a directory", unitsDir)
		}
		err := filepath.Walk(unitsDir, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".json") {
				return nil
			}
			result.UnitsTotal++
			upgraded, err := migrateJSONFile(path, "units.schema.json")
			if err != nil {
				return fmt.Errorf("failed to migrate unit %s: %w", fi.Name(), err)
			}
			if upgraded {
				result.UnitsUpgraded++
				result.TotalFilesModified++
				result.UpgradedFiles = append(result.UpgradedFiles, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed reading units directory %s: %w", unitsDir, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error accessing %s: %w", unitsDir, err)
	}

	// 4. Migrate cases/ (*.jsonl)
	casesDir := filepath.Join(dotDir, "cases")
	if info, err := os.Stat(casesDir); err == nil && info.IsDir() {
		err := filepath.Walk(casesDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
				return nil
			}
			result.CasesFilesTotal++
			linesTotal, linesUpgraded, fileUpgraded, err := migrateJSONLFile(path, "cases.schema.json")
			if err != nil {
				return fmt.Errorf("failed to migrate case file %s: %w", path, err)
			}
			result.CasesCount += linesTotal
			result.CasesUpgraded += linesUpgraded
			if fileUpgraded {
				result.TotalFilesModified++
				result.UpgradedFiles = append(result.UpgradedFiles, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// 5. Migrate reports/ (*.json and *.jsonl)
	reportsDir := filepath.Join(dotDir, "reports")
	if info, err := os.Stat(reportsDir); err == nil && info.IsDir() {
		err := filepath.Walk(reportsDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(info.Name(), ".json") {
				result.ReportsFilesTotal++
				upgraded, err := migrateJSONFile(path, "reports.schema.json")
				if err != nil {
					return fmt.Errorf("failed to migrate report %s: %w", path, err)
				}
				if upgraded {
					result.ReportsUpgraded++
					result.TotalFilesModified++
					result.UpgradedFiles = append(result.UpgradedFiles, path)
				}
			} else if strings.HasSuffix(info.Name(), ".jsonl") {
				result.ReportsFilesTotal++
				_, _, fileUpgraded, err := migrateJSONLFile(path, "reports.schema.json")
				if err != nil {
					return fmt.Errorf("failed to migrate report log %s: %w", path, err)
				}
				if fileUpgraded {
					result.ReportsUpgraded++
					result.TotalFilesModified++
					result.UpgradedFiles = append(result.UpgradedFiles, path)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// 6. Migrate evidence/ (*.json)
	evidenceDir := filepath.Join(dotDir, "evidence")
	if info, err := os.Stat(evidenceDir); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("expected evidence path %s to be a directory", evidenceDir)
		}
		err := filepath.Walk(evidenceDir, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".json") {
				return nil
			}
			result.EvidenceFilesTotal++
			upgraded, err := migrateJSONFile(path, "evidence.schema.json")
			if err != nil {
				return fmt.Errorf("failed to migrate evidence file %s: %w", fi.Name(), err)
			}
			if upgraded {
				result.EvidenceUpgraded++
				result.TotalFilesModified++
				result.UpgradedFiles = append(result.UpgradedFiles, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed reading evidence directory %s: %w", evidenceDir, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("error accessing %s: %w", evidenceDir, err)
	}

	return result, nil
}

// migrateJSONFile reads a JSON file under file lock, upgrades schema_version to v1 if it is v1alpha1,
// validates against the v1 schema, and writes it back atomically.
// Returns true if modified, false if already at v1.
func migrateJSONFile(filePath, schemaFileName string) (bool, error) {
	lockPath := filePath + ".lock"
	fileLock := flock.New(lockPath)
	if err := fileLock.Lock(); err != nil {
		return false, fmt.Errorf("failed to acquire file lock for %s: %w", filePath, err)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return false, fmt.Errorf("failed to unmarshal JSON from %s: %w", filePath, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return false, fmt.Errorf("unexpected trailing data in JSON file %s", filePath)
	}

	currentVersion, ok := doc["schema_version"].(string)
	if !ok {
		return false, fmt.Errorf("missing or invalid schema_version in %s", filePath)
	}

	if currentVersion == adapterkit.SchemaVersionV1 {
		// Already up to date, validate and return
		if err := schemas.ValidateJSON(adapterkit.SchemaVersionV1, schemaFileName, data); err != nil {
			return false, fmt.Errorf("validation failed for existing v1 file %s: %w", filePath, err)
		}
		return false, nil
	}

	if currentVersion != adapterkit.SchemaVersionV1Alpha1 {
		return false, fmt.Errorf("unsupported schema_version %q in %s", currentVersion, filePath)
	}

	// Upgrade version
	doc["schema_version"] = adapterkit.SchemaVersionV1

	newData, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, fmt.Errorf("failed to serialize upgraded JSON for %s: %w", filePath, err)
	}
	newData = append(newData, '\n')

	// Validate against target v1 schema before persisting
	if err := schemas.ValidateJSON(adapterkit.SchemaVersionV1, schemaFileName, newData); err != nil {
		return false, fmt.Errorf("schema validation failed for upgraded %s against %s: %w", filePath, schemaFileName, err)
	}

	// Persist atomically
	if err := writeTempSyncRename(filePath, newData); err != nil {
		return false, fmt.Errorf("atomic write failed for %s: %w", filePath, err)
	}

	return true, nil
}

// migrateJSONLFile reads a JSONL file under file lock, upgrading any v1alpha1 records to v1.
// Validates every record against the given v1 schema.
// Returns (totalLines, linesUpgraded, fileModified, error).
func migrateJSONLFile(filePath, schemaFileName string) (int, int, bool, error) {
	lockPath := filePath + ".lock"
	fileLock := flock.New(lockPath)
	if err := fileLock.Lock(); err != nil {
		return 0, 0, false, fmt.Errorf("failed to acquire file lock for %s: %w", filePath, err)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, false, fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	var outputLines [][]byte
	totalLines := 0
	linesUpgraded := 0
	fileModified := false

	for lineIdx, rawLine := range lines {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		totalLines++

		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()

		var record map[string]any
		if err := dec.Decode(&record); err != nil {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s has invalid JSON: %w", lineIdx+1, filePath, err)
		}
		var trailing any
		if err := dec.Decode(&trailing); err != io.EOF {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s has unexpected trailing data", lineIdx+1, filePath)
		}

		currentVersion, ok := record["schema_version"].(string)
		if !ok {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s is missing schema_version", lineIdx+1, filePath)
		}

		if currentVersion == adapterkit.SchemaVersionV1 {
			// Validate record
			if err := schemas.ValidateJSON(adapterkit.SchemaVersionV1, schemaFileName, line); err != nil {
				return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s fails v1 validation: %w", lineIdx+1, filePath, err)
			}
			outputLines = append(outputLines, line)
			continue
		}

		if currentVersion != adapterkit.SchemaVersionV1Alpha1 {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s has unsupported schema_version %q", lineIdx+1, filePath, currentVersion)
		}

		// Upgrade line
		record["schema_version"] = adapterkit.SchemaVersionV1
		newLine, err := json.Marshal(record)
		if err != nil {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s failed serialization: %w", lineIdx+1, filePath, err)
		}

		// Validate upgraded line
		if err := schemas.ValidateJSON(adapterkit.SchemaVersionV1, schemaFileName, newLine); err != nil {
			return totalLines, linesUpgraded, false, fmt.Errorf("line %d in %s fails v1 validation after upgrade: %w", lineIdx+1, filePath, err)
		}

		linesUpgraded++
		fileModified = true
		outputLines = append(outputLines, newLine)
	}

	if fileModified {
		var buf bytes.Buffer
		for _, l := range outputLines {
			buf.Write(l)
			buf.WriteByte('\n')
		}
		if err := writeTempSyncRename(filePath, buf.Bytes()); err != nil {
			return totalLines, linesUpgraded, false, fmt.Errorf("atomic write failed for %s: %w", filePath, err)
		}
	}

	return totalLines, linesUpgraded, fileModified, nil
}

// writeTempSyncRename atomically writes content to a temp file, fsyncs it, and renames it.
func writeTempSyncRename(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	baseName := filepath.Base(targetPath)
	fileMode := os.FileMode(0644)
	if fi, err := os.Stat(targetPath); err == nil {
		fileMode = fi.Mode()
	}

	tmpFile, err := os.CreateTemp(dir, "."+baseName+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", targetPath, err)
	}
	tmpName := tmpFile.Name()
	_ = os.Chmod(tmpName, fileMode)
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write data to %s: %w", tmpName, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to fsync %s: %w", tmpName, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmpName, err)
	}

	// Rename temp file to target path
	if err := os.Rename(tmpName, targetPath); err != nil {
		// Fallback for Windows where target file cannot be replaced atomically
		backupPath := targetPath + ".migrated.bak"
		_ = os.Remove(backupPath)
		if renameErr := os.Rename(targetPath, backupPath); renameErr == nil {
			if err2 := os.Rename(tmpName, targetPath); err2 != nil {
				_ = os.Rename(backupPath, targetPath)
				return fmt.Errorf("failed to rename %s to %s: %w", tmpName, targetPath, err2)
			}
			_ = os.Remove(backupPath)
		} else {
			return fmt.Errorf("failed to replace %s: %w", targetPath, err)
		}
	}

	return nil
}

// PrintSummary outputs a human-readable summary of the migration result to w.
func (r *MigrationResult) PrintSummary(w io.Writer) {
	if r.TotalFilesModified == 0 {
		fmt.Fprintf(w, "Workspace at %s is already up to date (%s).\nNo files were modified.\n", r.WorkspaceRoot, r.ToVersion)
		return
	}

	fmt.Fprintf(w, "Successfully upgraded workspace at %s from %s to %s:\n", r.WorkspaceRoot, r.FromVersion, r.ToVersion)
	if r.ManifestUpgraded {
		fmt.Fprintln(w, "  ✓ manifest.json upgraded")
	}
	if r.RulesUpgraded {
		fmt.Fprintln(w, "  ✓ rules.json upgraded")
	}
	if r.UnitsTotal > 0 {
		fmt.Fprintf(w, "  ✓ units: %d upgraded (%d total)\n", r.UnitsUpgraded, r.UnitsTotal)
	}
	if r.CasesFilesTotal > 0 {
		fmt.Fprintf(w, "  ✓ cases: %d case(s) upgraded across %d file(s)\n", r.CasesUpgraded, r.CasesFilesTotal)
	}
	if r.ReportsFilesTotal > 0 {
		fmt.Fprintf(w, "  ✓ reports: %d file(s) upgraded (%d total)\n", r.ReportsUpgraded, r.ReportsFilesTotal)
	}
	if r.EvidenceFilesTotal > 0 {
		fmt.Fprintf(w, "  ✓ evidence: %d record(s) upgraded (%d total)\n", r.EvidenceUpgraded, r.EvidenceFilesTotal)
	}
	fmt.Fprintf(w, "Total files updated: %d\nAll files validated successfully against %s schemas.\n", r.TotalFilesModified, r.ToVersion)
}
