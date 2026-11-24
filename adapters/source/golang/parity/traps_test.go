package parity_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mohammedrefat/go-anything/adapters/source/golang/parity"
	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoDirectiveBump_DetectionAndUnitCreation(t *testing.T) {
	oldMod := []byte("module example.com/app\n\ngo 1.21.6\n")
	newMod := []byte("module example.com/app\n\ngo 1.22.4\n")

	bump := parity.DetectGoDirectiveBump(oldMod, newMod)
	require.NotNil(t, bump)
	assert.True(t, bump.HasBump)
	assert.Equal(t, "1.21.6", bump.OldVersion)
	assert.Equal(t, "1.22.4", bump.NewVersion)
	assert.NotEmpty(t, bump.Risks)
	assert.Contains(t, bump.Risks[0], "loop variable scope")

	unit := parity.CreateGoBumpMigrationUnit(bump)
	require.NotNil(t, unit)
	assert.Equal(t, "go-directive-1.21.6-to-1.22.4", unit.ID)
	assert.Equal(t, adapterkit.StatePlanned, unit.State)
	assert.Equal(t, adapterkit.ProfileReplay, unit.EvidenceProfile)
}

func TestMapNormalization_EliminatesRandomIterationOrder(t *testing.T) {
	mapA := map[string]int{
		"zebra":  10,
		"apple":  20,
		"banana": 30,
	}

	mapB := map[string]int{
		"apple":  20,
		"banana": 30,
		"zebra":  10,
	}

	normA := parity.NormalizeMapOutput(mapA)
	normB := parity.NormalizeMapOutput(mapB)

	diff := cmp.Diff(normA, normB)
	assert.Empty(t, diff, "normalized map outputs should have identical sorted keys")
}

func TestDeterministicEnv_PredictableExecutions(t *testing.T) {
	fixedTime := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	env1 := parity.NewDeterministicEnv(42, fixedTime)
	env2 := parity.NewDeterministicEnv(42, fixedTime)

	// Verify clock
	assert.Equal(t, fixedTime, env1.Now())
	env1.Step(5 * time.Minute)
	assert.Equal(t, fixedTime.Add(5*time.Minute), env1.Now())

	// Verify seeded random sequence identity
	for i := 0; i < 20; i++ {
		assert.Equal(t, env1.Intn(1000), env2.Intn(1000))
	}

	// Verify context propagation
	ctx := env1.WithContext(context.Background())
	extracted := parity.EnvFromContext(ctx)
	require.NotNil(t, extracted)
	assert.Equal(t, env1.Now(), extracted.Now())
}

func TestDeepCopy_ProtectsMutableInputsAcrossVersions(t *testing.T) {
	type ComplexData struct {
		Name   string
		Items  []string
		Counts map[string]int
		Child  *ComplexData
	}

	orig := &ComplexData{
		Name:  "root",
		Items: []string{"a", "b", "c"},
		Counts: map[string]int{
			"key1": 100,
		},
		Child: &ComplexData{
			Name: "nested",
		},
	}

	args := []any{orig}
	clonedArgs := parity.DeepCopyArgs(args)
	require.Len(t, clonedArgs, 1)

	cloned := clonedArgs[0].(*ComplexData)

	// Mutate clone
	cloned.Name = "mutated"
	cloned.Items[0] = "mutated-a"
	cloned.Counts["key1"] = 999
	cloned.Child.Name = "mutated-child"

	// Assert original was untouched
	assert.Equal(t, "root", orig.Name)
	assert.Equal(t, "a", orig.Items[0])
	assert.Equal(t, 100, orig.Counts["key1"])
	assert.Equal(t, "nested", orig.Child.Name)
}

func TestGoroutineTracker_SynchronizationAndPanicCapture(t *testing.T) {
	gt := parity.NewGoroutineTracker()
	var counter atomic.Int32

	for i := 0; i < 5; i++ {
		gt.Go(func() {
			time.Sleep(5 * time.Millisecond)
			counter.Add(1)
		})
	}

	err := gt.Wait()
	require.NoError(t, err)
	assert.Equal(t, int32(5), counter.Load())
	assert.Equal(t, int64(0), gt.Active())

	// Test panic capture
	gtPanics := parity.NewGoroutineTracker()
	gtPanics.Go(func() {
		panic("goroutine exploded")
	})

	err = gtPanics.WaitWithTimeout(1 * time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "goroutine exploded")
}

func TestInspectSemanticRisks_FlagsHazards(t *testing.T) {
	pkg := &analyzer.PackageInfo{
		PkgPath: "example.com/dynamic/serializer",
		Imports: []string{"reflect", "unsafe", "fmt"},
		PublicAPIs: []string{
			"type Config",
			"func Serialize",
		},
	}

	risks := parity.InspectSemanticRisks(pkg)
	require.NotEmpty(t, risks)

	categories := make(map[string]bool)
	for _, r := range risks {
		categories[r.Category] = true
	}

	assert.True(t, categories["reflection"], "must flag reflect import")
	assert.True(t, categories["unsafe"], "must flag unsafe import")
	assert.True(t, categories["struct_tags"], "must flag struct tags characterization")
}
