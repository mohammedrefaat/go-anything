package library

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchFanOut_PreservesStrictOrderUnderConcurrency(t *testing.T) {
	const count = 1000
	inputs := make([]int, count)
	for i := 0; i < count; i++ {
		inputs[i] = i
	}

	ctx := context.Background()

	// Worker sleeps variable time to cause goroutines to finish out of order
	results, err := BatchFanOut(ctx, inputs, func(_ context.Context, item int) (string, error) {
		sleepMs := rand.Intn(5)
		if sleepMs > 0 {
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		}
		return fmt.Sprintf("val-%d", item), nil
	}, 16)

	require.NoError(t, err)
	require.Len(t, results, count)

	// Assert every item is at its exact expected index
	for i := 0; i < count; i++ {
		expected := fmt.Sprintf("val-%d", i)
		assert.Equal(t, expected, results[i], "item %d must preserve strict input order", i)
	}
}

func TestBatchFanOut_ErrorPropagation(t *testing.T) {
	inputs := []int{10, 20, 30, -1, 40, 50}

	results, err := BatchFanOut(context.Background(), inputs, func(_ context.Context, val int) (int, error) {
		if val < 0 {
			return 0, fmt.Errorf("negative value not allowed: %d", val)
		}
		return val * 2, nil
	}, 4)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative value not allowed: -1")
	_ = results
}

func TestFanOut_Simple(t *testing.T) {
	inputs := []string{"apple", "banana", "cherry"}
	outputs := FanOut(context.Background(), inputs, func(s string) int {
		return len(s)
	})

	assert.Equal(t, []int{5, 6, 6}, outputs)
}

func TestLibraryAdapter_ExecuteCase(t *testing.T) {
	adapter := NewAdapter("calculator")
	adapter.Register("unit-add", func(ctx context.Context, input any) (any, error) {
		vals, ok := input.([]any)
		if !ok || len(vals) < 2 {
			return nil, fmt.Errorf("invalid input")
		}
		a := int(vals[0].(float64))
		b := int(vals[1].(float64))
		return a + b, nil
	})

	assert.Equal(t, "library:calculator", adapter.DriverName())

	testCase := &adapterkit.Case{
		CaseID: "case-1",
		UnitID: "unit-add",
		Input: adapterkit.CaseInput{
			Body: []any{float64(10), float64(25)},
		},
	}

	out, err := adapter.ExecuteCase(context.Background(), testCase)
	require.NoError(t, err)
	assert.Equal(t, 0, out.ExitCode)
	assert.Equal(t, 35, out.Body)

	// Nonexistent unit returns error
	testCase2 := &adapterkit.Case{
		CaseID: "case-2",
		UnitID: "unit-nonexistent",
	}
	_, err = adapter.ExecuteCase(context.Background(), testCase2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no function registered")
}
