package frontdoor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// FallbackRecord represents the durable fallback state for a migration unit.
type FallbackRecord struct {
	UnitID       string    `json:"unit_id"`
	Count        int64     `json:"count"`
	LastFallback time.Time `json:"last_fallback"`
}

// FallbackFilePath returns the path to the durable fallback record file for unitID.
func FallbackFilePath(workspaceRoot, unitID string) string {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	return filepath.Join(workspaceRoot, ".go-anything", "local", unitID+".fallback")
}

// writeTempSyncRename atomically writes data to targetPath using a temp file, fsync, and rename.
func writeTempSyncRename(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	baseName := filepath.Base(targetPath)
	tmpFile, err := os.CreateTemp(dir, "."+baseName+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", targetPath, err)
	}
	tmpName := tmpFile.Name()
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
		// Fallback for Windows in case target file exists and replace fails
		_ = os.Remove(targetPath)
		if err := os.Rename(tmpName, targetPath); err != nil {
			return fmt.Errorf("failed to rename %s to %s: %w", tmpName, targetPath, err)
		}
	}

	return nil
}

// IncrementFallback atomically increments the durable fallback counter for unitID under .go-anything/local/.
func IncrementFallback(workspaceRoot, unitID string) (int64, error) {
	if unitID == "" {
		return 0, fmt.Errorf("unitID cannot be empty")
	}

	targetPath := FallbackFilePath(workspaceRoot, unitID)
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	lockPath := targetPath + ".lock"
	fileLock := flock.New(lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	locked, err := fileLock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return 0, fmt.Errorf("failed to acquire file lock for %s: %w", lockPath, err)
	}
	if !locked {
		return 0, fmt.Errorf("timed out acquiring file lock for %s", lockPath)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	var record FallbackRecord
	record.UnitID = unitID

	if data, err := os.ReadFile(targetPath); err == nil {
		if jsonErr := json.Unmarshal(data, &record); jsonErr != nil {
			// Fallback: parse raw integer string if previously written plain
			if parsedCount, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil {
				record.Count = parsedCount
			}
		}
	}

	record.Count++
	record.LastFallback = time.Now().UTC()

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("failed to marshal fallback record: %w", err)
	}

	if err := writeTempSyncRename(targetPath, data); err != nil {
		return 0, fmt.Errorf("failed to write fallback record: %w", err)
	}

	return record.Count, nil
}

// GetFallbackRecord returns the durable FallbackRecord for unitID, or nil if none exists.
func GetFallbackRecord(workspaceRoot, unitID string) (*FallbackRecord, error) {
	if unitID == "" {
		return nil, fmt.Errorf("unitID cannot be empty")
	}

	targetPath := FallbackFilePath(workspaceRoot, unitID)
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return nil, nil
	}

	lockPath := targetPath + ".lock"
	fileLock := flock.New(lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	locked, err := fileLock.TryRLockContext(ctx, 10*time.Millisecond)
	if err == nil && locked {
		defer func() {
			_ = fileLock.Unlock()
		}()
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read fallback file %s: %w", targetPath, err)
	}

	var record FallbackRecord
	record.UnitID = unitID

	if jsonErr := json.Unmarshal(data, &record); jsonErr != nil {
		if parsedCount, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); parseErr == nil {
			record.Count = parsedCount
		} else {
			return nil, fmt.Errorf("failed to parse fallback record from %s: %w", targetPath, jsonErr)
		}
	}

	return &record, nil
}

// GetFallbackCount returns the number of times fallback occurred for unitID.
// Returns 0 if no fallbacks have been recorded.
func GetFallbackCount(workspaceRoot, unitID string) (int64, error) {
	rec, err := GetFallbackRecord(workspaceRoot, unitID)
	if err != nil {
		return 0, err
	}
	if rec == nil {
		return 0, nil
	}
	return rec.Count, nil
}

// GetAllFallbackCounts returns a map of unitID -> fallback count for all units with recorded fallbacks.
func GetAllFallbackCounts(workspaceRoot string) (map[string]int64, error) {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	localDir := filepath.Join(workspaceRoot, ".go-anything", "local")
	counts := make(map[string]int64)

	entries, err := os.ReadDir(localDir)
	if err != nil {
		if os.IsNotExist(err) {
			return counts, nil
		}
		return nil, fmt.Errorf("failed to read local directory %s: %w", localDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".fallback") {
			unitID := strings.TrimSuffix(name, ".fallback")
			cnt, err := GetFallbackCount(workspaceRoot, unitID)
			if err != nil {
				return nil, fmt.Errorf("failed to read fallback count for %s: %w", unitID, err)
			}
			counts[unitID] = cnt
		}
	}

	return counts, nil
}

// ResetFallbackCount resets or removes the fallback record for unitID.
func ResetFallbackCount(workspaceRoot, unitID string) error {
	if unitID == "" {
		return fmt.Errorf("unitID cannot be empty")
	}
	targetPath := FallbackFilePath(workspaceRoot, unitID)
	lockPath := targetPath + ".lock"

	_ = os.Remove(targetPath)
	_ = os.Remove(lockPath)
	return nil
}
