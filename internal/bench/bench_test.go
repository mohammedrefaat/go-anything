package bench_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/internal/bench"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBenchmark_Measure(t *testing.T) {
	b := bench.New()

	workload := func() {
		// Allocate a slice and perform string operations
		s := make([]string, 100)
		for i := 0; i < len(s); i++ {
			s[i] = strings.Repeat("x", 10)
		}
	}

	metric := b.Measure(workload, 500)

	assert.Equal(t, 500, metric.Iterations)
	assert.Greater(t, metric.TotalDuration, time.Duration(0))
	assert.Greater(t, metric.DurationPerOpNS, float64(0))
	assert.Greater(t, metric.TotalAllocBytes, uint64(0))
	assert.Greater(t, metric.AllocBytesPerOp, uint64(0))
	assert.Greater(t, metric.AllocObjectsPerOp, uint64(0))
}

func TestBenchmark_Compare(t *testing.T) {
	b := bench.New()

	// Legacy workload: allocates strings via concatenation in a loop
	legacy := func() {
		res := ""
		for i := 0; i < 20; i++ {
			res += "abc"
		}
		_ = res
	}

	// Modern workload: uses strings.Builder (lower allocations)
	modern := func() {
		var builder strings.Builder
		builder.Grow(60)
		for i := 0; i < 20; i++ {
			builder.WriteString("abc")
		}
		_ = builder.String()
	}

	result := b.Compare("string_building", legacy, modern, 100)
	require.NotNil(t, result)
	assert.Equal(t, "string_building", result.Name)

	assert.Equal(t, 100, result.Legacy.Iterations)
	assert.Equal(t, 100, result.Modern.Iterations)

	// Modern builder should have fewer allocation bytes per op
	assert.LessOrEqual(t, result.Modern.AllocBytesPerOp, result.Legacy.AllocBytesPerOp)
	assert.LessOrEqual(t, result.AllocDeltaPercent, float64(0))
}
