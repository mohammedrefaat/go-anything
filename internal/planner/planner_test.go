package planner_test

import (
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/planner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanner_LinearDependencies(t *testing.T) {
	// A -> B -> C (A imports B, B imports C; C has no deps)
	graph := map[string][]string{
		"pkg/a": {"pkg/b"},
		"pkg/b": {"pkg/c"},
		"pkg/c": {},
	}

	p := planner.New()
	res := p.PlanWaves(graph)
	require.NotNil(t, res)

	assert.Equal(t, 3, res.TotalUnits)
	assert.Equal(t, 2, res.MaxWave)
	assert.Empty(t, res.Cycles)

	// Wave 0: pkg/c
	require.Len(t, res.Waves[0], 1)
	assert.Equal(t, "pkg/c", res.Waves[0][0].ID)

	// Wave 1: pkg/b
	require.Len(t, res.Waves[1], 1)
	assert.Equal(t, "pkg/b", res.Waves[1][0].ID)

	// Wave 2: pkg/a
	require.Len(t, res.Waves[2], 1)
	assert.Equal(t, "pkg/a", res.Waves[2][0].ID)
}

func TestPlanner_DiamondDependencies(t *testing.T) {
	// A -> B, A -> C, B -> D, C -> D
	graph := map[string][]string{
		"pkg/a": {"pkg/b", "pkg/c"},
		"pkg/b": {"pkg/d"},
		"pkg/c": {"pkg/d"},
		"pkg/d": {},
	}

	p := planner.New()
	res := p.PlanWaves(graph)
	require.NotNil(t, res)

	assert.Equal(t, 4, res.TotalUnits)
	assert.Equal(t, 2, res.MaxWave)

	// Wave 0: d
	require.Len(t, res.Waves[0], 1)
	assert.Equal(t, "pkg/d", res.Waves[0][0].ID)

	// Wave 1: b and c
	require.Len(t, res.Waves[1], 2)
	assert.Equal(t, "pkg/b", res.Waves[1][0].ID)
	assert.Equal(t, "pkg/c", res.Waves[1][1].ID)

	// Wave 2: a
	require.Len(t, res.Waves[2], 1)
	assert.Equal(t, "pkg/a", res.Waves[2][0].ID)
}

func TestPlanner_ImportCycleDetection(t *testing.T) {
	// Cycle: A <-> B (A imports B, B imports A)
	// C imports A (C -> A)
	graph := map[string][]string{
		"pkg/a": {"pkg/b"},
		"pkg/b": {"pkg/a"},
		"pkg/c": {"pkg/a"},
	}

	p := planner.New()
	res := p.PlanWaves(graph)
	require.NotNil(t, res)

	assert.Equal(t, 3, res.TotalUnits)
	require.Len(t, res.Cycles, 1, "exactly 1 cycle should be detected")
	assert.Equal(t, []string{"pkg/a", "pkg/b"}, res.Cycles[0])

	// Cycle units (a, b) should be grouped in Wave 0 (no external internal dependencies)
	require.Len(t, res.Waves[0], 2)
	assert.Equal(t, "pkg/a", res.Waves[0][0].ID)
	assert.Equal(t, "pkg/b", res.Waves[0][1].ID)

	assert.True(t, res.Waves[0][0].Metadata["in_cycle"].(bool))
	assert.True(t, res.Waves[0][1].Metadata["in_cycle"].(bool))

	// Unit c depends on cycle, so it is in Wave 1
	require.Len(t, res.Waves[1], 1)
	assert.Equal(t, "pkg/c", res.Waves[1][0].ID)
	assert.Contains(t, res.Waves[1][0].Dependencies, "pkg/a")
}

func TestPlanner_TarjanSCC_ComplexGraph(t *testing.T) {
	// SCC 1: {1, 2}
	// SCC 2: {3, 4, 5}
	// SCC 3: {6}
	graph := map[string][]string{
		"1": {"2"},
		"2": {"1", "3"},
		"3": {"4"},
		"4": {"5"},
		"5": {"3", "6"},
		"6": {},
	}

	sccs := planner.FindSCCs(graph)
	require.Len(t, sccs, 3)

	p := planner.New()
	res := p.PlanWaves(graph)

	// Leaf 6 is Wave 0
	require.Len(t, res.Waves[0], 1)
	assert.Equal(t, "6", res.Waves[0][0].ID)

	// Cycle {3, 4, 5} depends on 6 -> Wave 1
	require.Len(t, res.Waves[1], 3)

	// Cycle {1, 2} depends on 3 -> Wave 2
	require.Len(t, res.Waves[2], 2)
}
