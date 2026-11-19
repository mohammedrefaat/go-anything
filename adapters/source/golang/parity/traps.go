package parity

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// --- 1. Go Directive Bump Detection ---

var goDirectiveRegex = regexp.MustCompile(`(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

// GoDirectiveBump holds details on a Go directive version change in go.mod.
type GoDirectiveBump struct {
	OldVersion string   `json:"old_version"`
	NewVersion string   `json:"new_version"`
	HasBump    bool     `json:"has_bump"`
	Risks      []string `json:"risks,omitempty"`
}

// DetectGoDirectiveBump inspects old and new go.mod contents to identify toolchain directive changes.
func DetectGoDirectiveBump(oldModData, newModData []byte) *GoDirectiveBump {
	oldVer := extractGoVersion(oldModData)
	newVer := extractGoVersion(newModData)

	bump := &GoDirectiveBump{
		OldVersion: oldVer,
		NewVersion: newVer,
		HasBump:    oldVer != "" && newVer != "" && oldVer != newVer,
	}

	if bump.HasBump {
		bump.Risks = evaluateGoVersionRisks(oldVer, newVer)
	}

	return bump
}

func extractGoVersion(data []byte) string {
	matches := goDirectiveRegex.FindSubmatch(data)
	if len(matches) > 1 {
		return string(matches[1])
	}
	return ""
}

func evaluateGoVersionRisks(oldVer, newVer string) []string {
	var risks []string
	if strings.HasPrefix(oldVer, "1.21") && (strings.HasPrefix(newVer, "1.22") || strings.HasPrefix(newVer, "1.23") || strings.HasPrefix(newVer, "1.24") || strings.HasPrefix(newVer, "1.25") || strings.HasPrefix(newVer, "1.26")) {
		risks = append(risks, "loop variable scope changed to per-iteration in Go 1.22+")
		risks = append(risks, "net/http ServeMux routing patterns changed in Go 1.22+")
	}
	risks = append(risks, fmt.Sprintf("GODEBUG default behavior may differ between Go %s and Go %s", oldVer, newVer))
	return risks
}

// CreateGoBumpMigrationUnit creates a distinct migration unit for a go.mod directive bump.
func CreateGoBumpMigrationUnit(bump *GoDirectiveBump) *adapterkit.Unit {
	unitID := fmt.Sprintf("go-directive-%s-to-%s", bump.OldVersion, bump.NewVersion)
	return &adapterkit.Unit{
		SchemaVersion:   adapterkit.SchemaVersionV1Alpha1,
		ID:              unitID,
		Name:            fmt.Sprintf("Go directive bump (%s -> %s)", bump.OldVersion, bump.NewVersion),
		Wave:            0,
		State:           adapterkit.StatePlanned,
		EvidenceProfile: adapterkit.ProfileReplay,
		Risk:            "medium",
		Metadata: map[string]any{
			"bump_type":   "go_directive",
			"old_version": bump.OldVersion,
			"new_version": bump.NewVersion,
			"risks":       bump.Risks,
		},
	}
}

// --- 2. Map Iteration Order Normalization ---

// MapEntry holds a stringified key-value pair for sorted deterministic comparison.
type MapEntry struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// NormalizeMapOutput recursively transforms maps into sorted key-value slices
// so runtime random map iteration order does not produce spurious mismatches.
func NormalizeMapOutput(val any) any {
	if val == nil {
		return nil
	}

	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Map:
		keys := v.MapKeys()
		entries := make([]MapEntry, 0, len(keys))
		for _, k := range keys {
			normVal := NormalizeMapOutput(v.MapIndex(k).Interface())
			entries = append(entries, MapEntry{
				Key:   fmt.Sprintf("%v", k.Interface()),
				Value: normVal,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Key < entries[j].Key
		})
		return entries

	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = NormalizeMapOutput(v.Index(i).Interface())
		}
		return out

	default:
		return val
	}
}

// --- 3. Deterministic Environment (Time, Randomness, Environment) ---

type contextKey string

const deterministicEnvKey contextKey = "go_anything_deterministic_env"

// DeterministicEnv provides predictable clock, pseudorandom generator, and environment variables.
type DeterministicEnv struct {
	Clock time.Time
	Rand  *rand.Rand
	Env   map[string]string
	mu    sync.Mutex
}

// NewDeterministicEnv creates an environment initialized with a fixed time and seed.
func NewDeterministicEnv(seed int64, fixedTime time.Time) *DeterministicEnv {
	if fixedTime.IsZero() {
		fixedTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	return &DeterministicEnv{
		Clock: fixedTime,
		Rand:  rand.New(rand.NewSource(seed)),
		Env:   make(map[string]string),
	}
}

// Now returns the frozen or stepped clock.
func (e *DeterministicEnv) Now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Clock
}

// Step advances the clock by duration d.
func (e *DeterministicEnv) Step(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Clock = e.Clock.Add(d)
}

// Intn returns a deterministic pseudo-random integer.
func (e *DeterministicEnv) Intn(n int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Rand.Intn(n)
}

// Getenv returns an environment variable from the deterministic store.
func (e *DeterministicEnv) Getenv(key string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if v, ok := e.Env[key]; ok {
		return v
	}
	return os.Getenv(key)
}

// Setenv sets an environment variable in the deterministic store.
func (e *DeterministicEnv) Setenv(key, value string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Env[key] = value
}

// WithContext returns a new context containing the DeterministicEnv.
func (e *DeterministicEnv) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, deterministicEnvKey, e)
}

// EnvFromContext extracts the DeterministicEnv from ctx, or nil if not present.
func EnvFromContext(ctx context.Context) *DeterministicEnv {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(deterministicEnvKey).(*DeterministicEnv); ok {
		return v
	}
	return nil
}

// --- 4. Deep Copy of Mutable Arguments ---

// DeepCopy creates an isolated, deep copy of src so concurrent or sequential executions
// do not mutate shared inputs between legacy and modern versions.
func DeepCopy(src any) any {
	if src == nil {
		return nil
	}

	v := reflect.ValueOf(src)
	return deepCopyValue(v).Interface()
}

func deepCopyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		newPtr := reflect.New(v.Type().Elem())
		newPtr.Elem().Set(deepCopyValue(v.Elem()))
		return newPtr

	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			newSlice.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return newSlice

	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		newMap := reflect.MakeMap(v.Type())
		for _, key := range v.MapKeys() {
			kCopy := deepCopyValue(key)
			vCopy := deepCopyValue(v.MapIndex(key))
			newMap.SetMapIndex(kCopy, vCopy)
		}
		return newMap

	case reflect.Struct:
		newStruct := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := newStruct.Field(i)
			if field.CanSet() {
				field.Set(deepCopyValue(v.Field(i)))
			}
		}
		return newStruct

	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		return deepCopyValue(v.Elem())

	default:
		// Scalar types (int, float, bool, string, etc.) are value-copied
		return v
	}
}

// DeepCopyArgs clones a slice of function arguments.
func DeepCopyArgs(args []any) []any {
	if args == nil {
		return nil
	}
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = DeepCopy(a)
	}
	return out
}

// --- 5. Goroutine Tracker & Concurrency Synchronization ---

// GoroutineTracker waits for background goroutines and captures asynchronous panics.
type GoroutineTracker struct {
	wg     sync.WaitGroup
	active int64
	panics []any
	mu     sync.Mutex
}

// NewGoroutineTracker initializes a GoroutineTracker.
func NewGoroutineTracker() *GoroutineTracker {
	return &GoroutineTracker{}
}

// Go launches fn under tracker supervision.
func (gt *GoroutineTracker) Go(fn func()) {
	gt.wg.Add(1)
	atomic.AddInt64(&gt.active, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				gt.mu.Lock()
				gt.panics = append(gt.panics, r)
				gt.mu.Unlock()
			}
			atomic.AddInt64(&gt.active, -1)
			gt.wg.Done()
		}()
		fn()
	}()
}

// Wait blocks until all tracked goroutines complete.
func (gt *GoroutineTracker) Wait() error {
	gt.wg.Wait()
	gt.mu.Lock()
	defer gt.mu.Unlock()
	if len(gt.panics) > 0 {
		return fmt.Errorf("goroutine panicked: %v", gt.panics[0])
	}
	return nil
}

// WaitWithTimeout waits for goroutines to complete within timeout.
func (gt *GoroutineTracker) WaitWithTimeout(timeout time.Duration) error {
	done := make(chan struct{})
	go func() {
		gt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		gt.mu.Lock()
		defer gt.mu.Unlock()
		if len(gt.panics) > 0 {
			return fmt.Errorf("goroutine panicked: %v", gt.panics[0])
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for %d active goroutines", atomic.LoadInt64(&gt.active))
	}
}

// Active returns the count of running tracked goroutines.
func (gt *GoroutineTracker) Active() int64 {
	return atomic.LoadInt64(&gt.active)
}

// --- 6. Reflection, Struct Tag, and Unexported Characterization ---

// SemanticRisk identifies subtle Go semantic hazards that require characterization.
type SemanticRisk struct {
	Category    string `json:"category"`
	Target      string `json:"target"`
	Description string `json:"description"`
}

// InspectSemanticRisks analyzes package info for reflection, struct tags, and unexported behavior.
func InspectSemanticRisks(pkg *analyzer.PackageInfo) []SemanticRisk {
	var risks []SemanticRisk
	if pkg == nil {
		return risks
	}

	for _, imp := range pkg.Imports {
		if imp == "reflect" {
			risks = append(risks, SemanticRisk{
				Category:    "reflection",
				Target:      pkg.PkgPath,
				Description: "Package imports 'reflect': runtime type assertions and dynamic dispatch must be characterized",
			})
		}
		if imp == "unsafe" {
			risks = append(risks, SemanticRisk{
				Category:    "unsafe",
				Target:      pkg.PkgPath,
				Description: "Package imports 'unsafe': memory layouts may vary across toolchain versions",
			})
		}
	}

	for _, api := range pkg.PublicAPIs {
		if strings.HasPrefix(api, "type ") {
			typeName := strings.TrimPrefix(api, "type ")
			risks = append(risks, SemanticRisk{
				Category:    "struct_tags",
				Target:      typeName,
				Description: fmt.Sprintf("Type %s: verify JSON/YAML/SQL struct tags match between legacy and modern", typeName),
			})
		}
	}

	return risks
}
