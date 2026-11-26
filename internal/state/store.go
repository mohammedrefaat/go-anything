package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
)

const (
	WorkspaceDirName = ".go-anything"
	UnitsDirName     = "units"
	LocalDirName     = "local"
	CasesDirName     = "cases"
	ReportsDirName   = "reports"
	EvidenceDirName  = "evidence"
	ManifestFileName = "manifest.json"
	RulesFileName    = "rules.json"
)

// Store manages durable workspace persistence under .go-anything/.
type Store struct {
	workspaceRoot string
	rootDir       string
	unitsDir      string
	localDir      string
	casesDir      string
	reportsDir    string
	evidenceDir   string
}

// NewStore initializes a Store pointing to the workspace root.
func NewStore(workspaceRoot string) *Store {
	rootDir := filepath.Join(workspaceRoot, WorkspaceDirName)
	return &Store{
		workspaceRoot: workspaceRoot,
		rootDir:       rootDir,
		unitsDir:      filepath.Join(rootDir, UnitsDirName),
		localDir:      filepath.Join(rootDir, LocalDirName),
		casesDir:      filepath.Join(rootDir, CasesDirName),
		reportsDir:    filepath.Join(rootDir, ReportsDirName),
		evidenceDir:   filepath.Join(rootDir, EvidenceDirName),
	}
}

// WorkspaceRoot returns the path to the workspace root.
func (s *Store) WorkspaceRoot() string {
	return s.workspaceRoot
}

// RootDir returns the absolute path to .go-anything/.
func (s *Store) RootDir() string {
	return s.rootDir
}

// UnitsDir returns the path to .go-anything/units/.
func (s *Store) UnitsDir() string {
	return s.unitsDir
}

// LocalDir returns the path to .go-anything/local/.
func (s *Store) LocalDir() string {
	return s.localDir
}

// EvidenceDir returns the path to .go-anything/evidence/.
func (s *Store) EvidenceDir() string {
	return s.evidenceDir
}

// CasesDir returns the path to .go-anything/cases/.
func (s *Store) CasesDir() string {
	return s.casesDir
}

// ReportsDir returns the path to .go-anything/reports/.
func (s *Store) ReportsDir() string {
	return s.reportsDir
}

// writeTempSyncRename writes data to a temporary file, calls fsync, and atomically renames it to targetPath.
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

// atomicWriteFile persists data atomically: creates temp file, syncs to disk, and renames under an OS file lock.
func atomicWriteFile(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	lockPath := targetPath + ".lock"
	fileLock := flock.New(lockPath)
	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("failed to acquire OS file lock for %s: %w", targetPath, err)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	return writeTempSyncRename(targetPath, data)
}

// atomicReadFile reads a file while holding an OS file lock.
func atomicReadFile(targetPath string) ([]byte, error) {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	lockPath := targetPath + ".lock"
	fileLock := flock.New(lockPath)
	if err := fileLock.RLock(); err != nil {
		return nil, fmt.Errorf("failed to acquire OS read lock for %s: %w", targetPath, err)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	return os.ReadFile(targetPath)
}

// SaveManifest writes .go-anything/manifest.json atomically.
func (s *Store) SaveManifest(m *adapterkit.Manifest) error {
	if m.SchemaVersion == "" {
		m.SchemaVersion = adapterkit.SchemaVersionV1Alpha1
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.rootDir, ManifestFileName), data)
}

// GetManifest reads .go-anything/manifest.json.
func (s *Store) GetManifest() (*adapterkit.Manifest, error) {
	path := filepath.Join(s.rootDir, ManifestFileName)
	data, err := atomicReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest.json not found: %w", err)
		}
		return nil, err
	}
	var m adapterkit.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest.json: %w", err)
	}
	return &m, nil
}

// SaveRules writes .go-anything/rules.json atomically.
func (s *Store) SaveRules(r *adapterkit.Rules) error {
	if r.SchemaVersion == "" {
		r.SchemaVersion = adapterkit.SchemaVersionV1Alpha1
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal rules: %w", err)
	}
	return atomicWriteFile(filepath.Join(s.rootDir, RulesFileName), data)
}

// GetRules reads .go-anything/rules.json.
func (s *Store) GetRules() (*adapterkit.Rules, error) {
	path := filepath.Join(s.rootDir, RulesFileName)
	data, err := atomicReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("rules.json not found: %w", err)
		}
		return nil, err
	}
	var r adapterkit.Rules
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rules.json: %w", err)
	}
	return &r, nil
}

func (s *Store) unitFilePath(unitID string) string {
	return filepath.Join(s.unitsDir, unitID+".json")
}

func (s *Store) leaseFilePath(unitID string) string {
	return filepath.Join(s.localDir, unitID+".lease")
}

func (s *Store) evidenceFilePath(evidenceID string) string {
	return filepath.Join(s.evidenceDir, evidenceID+".json")
}

// SaveUnit writes the unit state file under .go-anything/units/<id>.json atomically.
func (s *Store) SaveUnit(u *adapterkit.Unit) error {
	if u.ID == "" {
		return fmt.Errorf("cannot save unit with empty ID")
	}
	if u.SchemaVersion == "" {
		u.SchemaVersion = adapterkit.SchemaVersionV1Alpha1
	}
	u.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal unit %s: %w", u.ID, err)
	}
	return atomicWriteFile(s.unitFilePath(u.ID), data)
}

// GetUnit reads the unit state file. If the unit is currently in progress but its local lease has expired,
// it automatically returns the unit to Ready per ADR-002 and updates persistence.
func (s *Store) GetUnit(unitID string) (*adapterkit.Unit, error) {
	path := s.unitFilePath(unitID)
	data, err := atomicReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrUnitNotFound
		}
		return nil, err
	}

	var u adapterkit.Unit
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("failed to parse unit %s: %w", unitID, err)
	}

	// Automatic recovery: If unit is "In progress", check lease status
	if u.State == adapterkit.StateInProgress {
		lease, err := s.GetLease(unitID)
		if err != nil || lease == nil || lease.IsExpired() {
			// Lease expired or missing -> auto-recover to Ready
			u.State = adapterkit.StateReady
			_ = s.SaveUnit(&u)
			_ = s.removeLease(unitID)
		}
	}

	return &u, nil
}

// ListUnits returns all units in the workspace.
func (s *Store) ListUnits() ([]*adapterkit.Unit, error) {
	if _, err := os.Stat(s.unitsDir); os.IsNotExist(err) {
		return nil, nil
	}

	var units []*adapterkit.Unit
	err := filepath.WalkDir(s.unitsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		rel, err := filepath.Rel(s.unitsDir, path)
		if err != nil {
			return nil
		}
		unitID := filepath.ToSlash(strings.TrimSuffix(rel, ".json"))
		u, err := s.GetUnit(unitID)
		if err != nil {
			return nil
		}
		units = append(units, u)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return units, nil
}

// AcquireLease attempts to claim a time-bounded local lease for a unit in .go-anything/local/<unit>.lease.
func (s *Store) AcquireLease(unitID, owner string, ttl time.Duration) (*Lease, error) {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	leasePath := s.leaseFilePath(unitID)
	lockPath := leasePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lease directory: %w", err)
	}
	fileLock := flock.New(lockPath)
	if err := fileLock.Lock(); err != nil {
		return nil, fmt.Errorf("failed to lock lease: %w", err)
	}
	defer func() {
		_ = fileLock.Unlock()
	}()

	existing, err := s.readLeaseDirect(unitID)
	now := time.Now().UTC()
	if err == nil && existing != nil && existing.IsValid() && existing.Owner != owner {
		return nil, ErrLeaseActive
	}

	lease := &Lease{
		UnitID:    unitID,
		Owner:     owner,
		ClaimedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	data, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal lease: %w", err)
	}

	if err := writeTempSyncRename(leasePath, data); err != nil {
		return nil, fmt.Errorf("failed to persist lease: %w", err)
	}

	return lease, nil
}

// GetLease retrieves the current lease for a unit if present.
func (s *Store) GetLease(unitID string) (*Lease, error) {
	path := s.leaseFilePath(unitID)
	data, err := atomicReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l Lease
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) readLeaseDirect(unitID string) (*Lease, error) {
	path := s.leaseFilePath(unitID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var l Lease
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// ReleaseLease removes the lease if held by owner.
func (s *Store) ReleaseLease(unitID, owner string) error {
	lease, err := s.GetLease(unitID)
	if err != nil || lease == nil {
		return nil
	}
	if lease.Owner != owner && lease.IsValid() {
		return ErrLeaseOwnerMismatch
	}
	return s.removeLease(unitID)
}

func (s *Store) removeLease(unitID string) error {
	path := s.leaseFilePath(unitID)
	_ = os.Remove(path + ".lock")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// SaveEvidence stores cryptographic verification records under .go-anything/evidence/<id>.json.
func (s *Store) SaveEvidence(ev *adapterkit.Evidence) error {
	if ev.EvidenceID == "" {
		return fmt.Errorf("cannot save evidence with empty EvidenceID")
	}
	if !shaRegex.MatchString(ev.CommitSHA) {
		return fmt.Errorf("commit_sha must be a 40-character hex string, got %q", ev.CommitSHA)
	}
	if ev.SchemaVersion == "" {
		ev.SchemaVersion = adapterkit.SchemaVersionV1Alpha1
	}
	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal evidence: %w", err)
	}
	return atomicWriteFile(s.evidenceFilePath(ev.EvidenceID), data)
}

// GetEvidence retrieves an evidence record by ID.
func (s *Store) GetEvidence(evidenceID string) (*adapterkit.Evidence, error) {
	path := s.evidenceFilePath(evidenceID)
	data, err := atomicReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("evidence not found: %w", err)
		}
		return nil, err
	}
	var ev adapterkit.Evidence
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("failed to unmarshal evidence: %w", err)
	}
	return &ev, nil
}

// ListEvidenceForUnit returns all evidence records tied to a unit.
func (s *Store) ListEvidenceForUnit(unitID string) ([]*adapterkit.Evidence, error) {
	if _, err := os.Stat(s.evidenceDir); os.IsNotExist(err) {
		return nil, nil
	}

	var results []*adapterkit.Evidence
	err := filepath.WalkDir(s.evidenceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		rel, err := filepath.Rel(s.evidenceDir, path)
		if err != nil {
			return nil
		}
		evID := filepath.ToSlash(strings.TrimSuffix(rel, ".json"))
		ev, err := s.GetEvidence(evID)
		if err != nil {
			return nil
		}
		if ev.UnitID == unitID {
			results = append(results, ev)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// GetEvidenceForCommit finds the evidence record for a unit stamped with the given commit SHA.
func (s *Store) GetEvidenceForCommit(unitID, commitSHA string) (*adapterkit.Evidence, error) {
	list, err := s.ListEvidenceForUnit(unitID)
	if err != nil {
		return nil, err
	}
	for _, ev := range list {
		if strings.EqualFold(ev.CommitSHA, commitSHA) {
			return ev, nil
		}
	}
	return nil, fmt.Errorf("no evidence found for unit %s at commit %s", unitID, commitSHA)
}

// GetLatestEvidenceForUnit returns the most recent evidence record for a unit.
func (s *Store) GetLatestEvidenceForUnit(unitID string) (*adapterkit.Evidence, error) {
	list, err := s.ListEvidenceForUnit(unitID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no evidence found for unit %s", unitID)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].VerifiedAt > list[j].VerifiedAt
	})
	return list[0], nil
}
