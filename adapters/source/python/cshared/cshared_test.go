package cshared_test

import (
	"context"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/source/python/cshared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluator_AllHazardsEvaluated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	evaluator := cshared.NewEvaluator()
	result, err := evaluator.EvaluateAll(ctx)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.EvaluatedAt)
	assert.NotEmpty(t, result.GoVersion)
	assert.NotEmpty(t, result.TargetOS)
	assert.NotEmpty(t, result.TargetArch)

	// Must evaluate all 3 mandatory hazard categories
	require.Len(t, result.Evaluations, 3)

	hazardsFound := make(map[cshared.HazardType]bool)
	for _, ev := range result.Evaluations {
		hazardsFound[ev.Hazard] = true
		assert.NotEmpty(t, ev.Title)
		assert.NotEmpty(t, ev.Summary)
		assert.NotEmpty(t, ev.TechnicalDetails)
		assert.NotEmpty(t, ev.FailureScenarios)
		assert.NotEmpty(t, ev.MitigationStatus)
		assert.Equal(t, cshared.StatusFailed, ev.Status)
	}

	assert.True(t, hazardsFound[cshared.HazardGIL], "expected GIL hazard evaluation")
	assert.True(t, hazardsFound[cshared.HazardFork], "expected fork safety hazard evaluation")
	assert.True(t, hazardsFound[cshared.HazardSignal], "expected signal handling hazard evaluation")

	// Must reject production readiness for c-shared
	assert.False(t, result.ProductionReady, "c-shared must not be marked production ready")
	assert.Contains(t, result.Recommendation, "REJECTED")
	assert.Contains(t, result.Recommendation, "Subprocess and HTTP transports are certified as production-grade")
}

func TestEvaluator_GILHazard(t *testing.T) {
	ctx := context.Background()
	evaluator := cshared.NewEvaluator()
	ev := evaluator.EvaluateGIL(ctx)

	assert.Equal(t, cshared.HazardGIL, ev.Hazard)
	assert.Equal(t, cshared.SeverityHigh, ev.Severity)
	assert.Equal(t, cshared.StatusFailed, ev.Status)
	assert.Contains(t, ev.Summary, "CPython OS threads")
}

func TestEvaluator_ForkSafetyHazard(t *testing.T) {
	ctx := context.Background()
	evaluator := cshared.NewEvaluator()
	ev := evaluator.EvaluateForkSafety(ctx)

	assert.Equal(t, cshared.HazardFork, ev.Hazard)
	assert.Equal(t, cshared.SeverityFatal, ev.Severity)
	assert.Equal(t, cshared.StatusFailed, ev.Status)
	assert.Contains(t, ev.Summary, "POSIX fork()")
	assert.Contains(t, ev.TechnicalDetails[2], "Gunicorn")
}

func TestEvaluator_SignalHandlingHazard(t *testing.T) {
	ctx := context.Background()
	evaluator := cshared.NewEvaluator()
	ev := evaluator.EvaluateSignalHandling(ctx)

	assert.Equal(t, cshared.HazardSignal, ev.Hazard)
	assert.Equal(t, cshared.SeverityHigh, ev.Severity)
	assert.Equal(t, cshared.StatusFailed, ev.Status)
	assert.Contains(t, ev.Summary, "SIGSEGV")
	assert.Contains(t, ev.Summary, "KeyboardInterrupt")
}
