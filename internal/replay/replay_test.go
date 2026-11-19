package replay_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/replay"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReplayDriver simulates execution with artificial delays.
type mockReplayDriver struct {
	responder func(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error)
}

func (m *mockReplayDriver) DriverName() string {
	return "mock-driver"
}

func (m *mockReplayDriver) ExecuteCase(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
	if m.responder != nil {
		return m.responder(ctx, c)
	}
	// Default echo output
	return &c.ExpectedOutput, nil
}

func (m *mockReplayDriver) Close() error {
	return nil
}

func generateTestCases(count int) []*adapterkit.Case {
	cases := make([]*adapterkit.Case, count)
	// Create cases with unordered IDs so we can assert sorting
	r := rand.New(rand.NewSource(42))
	perm := r.Perm(count)

	for i, idx := range perm {
		caseID := fmt.Sprintf("case-%03d", idx)
		cases[i] = &adapterkit.Case{
			SchemaVersion: adapterkit.SchemaVersionV1Alpha1,
			CaseID:        caseID,
			UnitID:        "checkout",
			Kind:          adapterkit.KindHappy,
			Input: adapterkit.CaseInput{
				Method: "POST",
				Path:   fmt.Sprintf("/item/%d", idx),
			},
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       fmt.Sprintf(`{"item_id": %d, "status": "added"}`, idx),
			},
		}
	}
	return cases
}

func TestConcurrentDeterministicReplay_ByteIdenticalReports(t *testing.T) {
	cases := generateTestCases(30)
	ctx := context.Background()

	engine := replay.New(
		replay.WithJobs(4),
		replay.WithTimeout(5*time.Second),
	)

	// Driver introduces random jitter to ensure goroutines complete out of order
	driver := &mockReplayDriver{
		responder: func(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
			sleepMs := rand.Intn(10) + 1
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
			return &c.ExpectedOutput, nil
		},
	}

	// Run 1
	rep1, err := engine.ReplayCases(ctx, "checkout", cases, driver)
	require.NoError(t, err)
	require.True(t, rep1.PassedCases == rep1.TotalCases)

	// Run 2
	rep2, err := engine.ReplayCases(ctx, "checkout", cases, driver)
	require.NoError(t, err)

	// Run 3
	rep3, err := engine.ReplayCases(ctx, "checkout", cases, driver)
	require.NoError(t, err)

	// Verify sorting by case_id
	for i := 0; i < len(rep1.Results)-1; i++ {
		assert.True(t, rep1.Results[i].CaseID < rep1.Results[i+1].CaseID,
			"results must be strictly ordered by case_id: %s < %s",
			rep1.Results[i].CaseID, rep1.Results[i+1].CaseID)
	}

	// Normalize duration for byte-level comparison of reports
	for i := range rep1.Results {
		rep1.Results[i].DurationMS = 0
		rep2.Results[i].DurationMS = 0
		rep3.Results[i].DurationMS = 0
	}

	json1, err := rep1.ToJSON()
	require.NoError(t, err)
	json2, err := rep2.ToJSON()
	require.NoError(t, err)
	json3, err := rep3.ToJSON()
	require.NoError(t, err)

	// Assert byte-identical outputs across repeated runs (M1 Exit Gate requirement)
	assert.True(t, bytes.Equal(json1, json2), "Run 1 and Run 2 reports must be byte-identical")
	assert.True(t, bytes.Equal(json2, json3), "Run 2 and Run 3 reports must be byte-identical")

	// Verify markdown output
	md1 := rep1.ToMarkdown()
	md2 := rep2.ToMarkdown()
	assert.Equal(t, md1, md2)
	assert.Contains(t, md1, "### Replay Report: checkout")
	assert.Contains(t, md1, "case-000")
	assert.Contains(t, md1, "case-029")
}

func TestReplay_DetectsMismatches(t *testing.T) {
	ctx := context.Background()
	engine := replay.New(replay.WithJobs(2))

	cases := []*adapterkit.Case{
		{
			CaseID: "case-pass",
			UnitID: "auth",
			Kind:   adapterkit.KindHappy,
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       `{"status":"ok"}`,
			},
		},
		{
			CaseID: "case-fail",
			UnitID: "auth",
			Kind:   adapterkit.KindEdge,
			ExpectedOutput: adapterkit.CaseOutput{
				StatusCode: 200,
				Body:       `{"status":"ok"}`,
			},
		},
	}

	driver := &mockReplayDriver{
		responder: func(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
			if c.CaseID == "case-fail" {
				// Inject status code discrepancy
				return &adapterkit.CaseOutput{
					StatusCode: 500,
					Body:       `{"error":"internal"}`,
				}, nil
			}
			return &c.ExpectedOutput, nil
		},
	}

	report, err := engine.ReplayCases(ctx, "auth", cases, driver)
	require.NoError(t, err)

	assert.Equal(t, 2, report.TotalCases)
	assert.Equal(t, 1, report.PassedCases)
	assert.Equal(t, 1, report.FailedCases)
	assert.Contains(t, report.Summary, "1/2 cases failed")

	// Results must be sorted: case-fail, case-pass
	assert.Equal(t, "case-fail", report.Results[0].CaseID)
	assert.False(t, report.Results[0].Passed)
	assert.NotEmpty(t, report.Results[0].Mismatches)

	assert.Equal(t, "case-pass", report.Results[1].CaseID)
	assert.True(t, report.Results[1].Passed)
}

func TestReplaySequences_StatefulExecution(t *testing.T) {
	ctx := context.Background()
	engine := replay.New(replay.WithJobs(3))

	// Two independent stateful sequences
	seq1 := []*adapterkit.Case{
		{
			CaseID:         "seq1-step1-login",
			UnitID:         "shop",
			ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: `{"step": 1}`},
		},
		{
			CaseID:         "seq1-step2-checkout",
			UnitID:         "shop",
			ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: `{"step": 2}`},
		},
	}

	seq2 := []*adapterkit.Case{
		{
			CaseID:         "seq2-step1-login",
			UnitID:         "shop",
			ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: `{"step": 1}`},
		},
		{
			CaseID:         "seq2-step2-checkout",
			UnitID:         "shop",
			ExpectedOutput: adapterkit.CaseOutput{StatusCode: 200, Body: `{"step": 2}`},
		},
	}

	serverCreated := 0
	factory := func(ctx context.Context, port int, workDir string) (adapterkit.Replay, error) {
		assert.Greater(t, port, 0)
		assert.DirExists(t, workDir)
		serverCreated++
		return &mockReplayDriver{}, nil
	}

	report, err := engine.ReplaySequences(ctx, "shop", [][]*adapterkit.Case{seq1, seq2}, factory)
	require.NoError(t, err)

	assert.Equal(t, 4, report.TotalCases)
	assert.Equal(t, 4, report.PassedCases)
	assert.Equal(t, 0, report.FailedCases)

	// Verify all 4 results sorted by CaseID
	expectedOrder := []string{"seq1-step1-login", "seq1-step2-checkout", "seq2-step1-login", "seq2-step2-checkout"}
	for i, expectedID := range expectedOrder {
		assert.Equal(t, expectedID, report.Results[i].CaseID)
	}
}

func TestGetFreePort(t *testing.T) {
	port, err := replay.GetFreePort()
	require.NoError(t, err)
	assert.Greater(t, port, 1024)
	assert.Less(t, port, 65536)
}
