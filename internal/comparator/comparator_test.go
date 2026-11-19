package comparator_test

import (
	"os"
	"testing"

	"github.com/mohammedrefat/go-anything/internal/comparator"
	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModeExact(t *testing.T) {
	c := comparator.New()

	tests := []struct {
		name     string
		expected string
		actual   string
		pass     bool
	}{
		{"identical strings", "hello world", "hello world", true},
		{"different strings", "hello", "world", false},
		{"empty strings", "", "", true},
		{"case sensitivity", "abc", "ABC", false},
		{"whitespace sensitivity", "hello\n", "hello\r\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := c.Compare(comparator.ModeExact, tt.expected, tt.actual)
			require.NoError(t, err)
			assert.Equal(t, tt.pass, res.Passed)
			if !tt.pass {
				assert.NotEmpty(t, res.Mismatches)
			}
		})
	}
}

func TestModeNumericTolerance(t *testing.T) {
	c := comparator.New(comparator.WithNumericDelta(0.01))

	tests := []struct {
		name     string
		expected string
		actual   string
		pass     bool
	}{
		{"exact match", "3.14159", "3.14159", true},
		{"within tolerance", "10.005", "10.009", true},
		{"exceeds tolerance", "10.00", "10.05", false},
		{"integer string within tolerance", "100", "100.005", true},
		{"invalid number", "abc", "100", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := c.Compare(comparator.ModeNumericTolerance, tt.expected, tt.actual)
			require.NoError(t, err)
			assert.Equal(t, tt.pass, res.Passed)
		})
	}
}

func TestModeErrorEquivalence(t *testing.T) {
	c := comparator.New()

	tests := []struct {
		name    string
		expErr  string
		actErr  string
		expCode int
		actCode int
		pass    bool
	}{
		{"identical error and exit code", "file not found", "file not found", 1, 1, true},
		{"case-insensitive match", "Permission Denied", "permission denied", 2, 2, true},
		{"substring core error match", "error: connection reset by peer", "connection reset by peer", 1, 1, true},
		{"exit code mismatch", "failed", "failed", 0, 1, false},
		{"completely different error", "syntax error", "timeout exceeded", 1, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := &adapterkit.CaseOutput{ErrorMessage: tt.expErr, ExitCode: tt.expCode}
			act := &adapterkit.CaseOutput{ErrorMessage: tt.actErr, ExitCode: tt.actCode}

			res, err := c.CompareCaseOutput(comparator.ModeErrorEquivalence, exp, act)
			require.NoError(t, err)
			assert.Equal(t, tt.pass, res.Passed)
		})
	}
}

func TestSemanticTrap_MissingVsNull(t *testing.T) {
	c := comparator.New()

	t.Run("expected null but key missing in actual", func(t *testing.T) {
		expected := `{"user_id": 1, "metadata": null}`
		actual := `{"user_id": 1}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed, "should detect missing vs null discrepancy")
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "key \"metadata\" is missing in actual (expected null)")
	})

	t.Run("expected missing key but present as null in actual", func(t *testing.T) {
		expected := `{"user_id": 1}`
		actual := `{"user_id": 1, "metadata": null}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed, "should detect present null vs missing discrepancy")
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "key \"metadata\" is present as null in actual (expected missing)")
	})

	t.Run("both null matches", func(t *testing.T) {
		expected := `{"user_id": 1, "metadata": null}`
		actual := `{"user_id": 1, "metadata": null}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})
}

func TestSemanticTrap_IntegerVsFloat(t *testing.T) {
	c := comparator.New()

	t.Run("expected integer 42 vs float 42.0", func(t *testing.T) {
		expected := `{"count": 42}`
		actual := `{"count": 42.0}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed, "must distinguish integer from float representation in JSON")
		require.Len(t, res.Mismatches, 1)
		assert.Contains(t, res.Mismatches[0].Message, "number representation mismatch")
	})

	t.Run("both integers match", func(t *testing.T) {
		expected := `{"count": 42}`
		actual := `{"count": 42}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})

	t.Run("both floats match", func(t *testing.T) {
		expected := `{"rating": 4.5}`
		actual := `{"rating": 4.5}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.True(t, res.Passed)
	})
}

func TestSemanticTrap_OrderedVsUnorderedCollections(t *testing.T) {
	t.Run("objects ignore key order by default (unordered)", func(t *testing.T) {
		c := comparator.New()
		expected := `{"b": 2, "a": 1, "c": 3}`
		actual := `{"a": 1, "c": 3, "b": 2}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.True(t, res.Passed, "JSON map keys are unordered")
	})

	t.Run("arrays are ordered by default: different order fails", func(t *testing.T) {
		c := comparator.New()
		expected := `{"items": [1, 2, 3]}`
		actual := `{"items": [3, 2, 1]}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.False(t, res.Passed, "default array comparison must preserve order")
	})

	t.Run("array with sort_array normalizer passes regardless of order", func(t *testing.T) {
		rules := &adapterkit.Rules{
			Normalizers: []adapterkit.NormalizerRule{
				{Path: "items", Type: "sort_array"},
			},
		}
		c := comparator.New(comparator.WithRules(rules))

		expected := `{"items": ["apple", "banana", "cherry"]}`
		actual := `{"items": ["cherry", "apple", "banana"]}`

		res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
		require.NoError(t, err)
		assert.True(t, res.Passed, "sort_array normalizer must allow unordered array comparison")
	})
}

func TestWildcardAndNormalizerRules(t *testing.T) {
	rules := &adapterkit.Rules{
		Wildcards: []adapterkit.WildcardRule{
			{Path: "response.request_id", Type: "uuid"},
			{Path: "response.created_at", Type: "timestamp"},
			{Path: "response.token", Type: "regex", Pattern: `^tok_[a-z0-9]+$`},
		},
		Normalizers: []adapterkit.NormalizerRule{
			{Path: "user.name", Type: "trim_whitespace"},
			{Path: "user.email", Type: "lower_case"},
		},
	}
	c := comparator.New(comparator.WithRules(rules))

	expected := `{
		"response": {
			"request_id": "12345678-1234-1234-1234-123456789abc",
			"created_at": "2026-11-19T10:00:00Z",
			"token": "tok_abcdef123",
			"status": "ok"
		},
		"user": {
			"name": "Alice Smith",
			"email": "alice@example.com"
		}
	}`

	// Actual has different UUID, different RFC3339 timestamp, different regex matching token, extra whitespace in name, uppercase email
	actual := `{
		"response": {
			"request_id": "87654321-4321-4321-4321-cba987654321",
			"created_at": "2026-11-19T10:15:30Z",
			"token": "tok_999xyz888",
			"status": "ok"
		},
		"user": {
			"name": "  Alice Smith  ",
			"email": "ALICE@EXAMPLE.COM"
		}
	}`

	res, err := c.Compare(comparator.ModeStructuralJSON, expected, actual)
	require.NoError(t, err)
	assert.True(t, res.Passed, "wildcards and normalizers must neutralize dynamic discrepancies")
}

func TestModeHTTPContract(t *testing.T) {
	rules := &adapterkit.Rules{
		Wildcards: []adapterkit.WildcardRule{
			{Path: "headers.date", Type: "timestamp"},
			{Path: "headers.x-request-id", Type: "uuid"},
		},
	}
	c := comparator.New(comparator.WithRules(rules))

	exp := &adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Date":         "Wed, 19 Nov 2026 10:00:00 GMT",
			"X-Request-Id": "11111111-2222-3333-4444-555555555555",
		},
		Body: `{"status":"active","count":10}`,
	}

	act := &adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"content-type": "application/json",
			"date":         "Wed, 19 Nov 2026 10:05:00 GMT",
			"x-request-id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		},
		Body: `{"count":10,"status":"active"}`,
	}

	res, err := c.CompareCaseOutput(comparator.ModeHTTPContract, exp, act)
	require.NoError(t, err)
	assert.True(t, res.Passed, "HTTP contract with matching status, headers, and body passes")

	// Discrepancy in status code
	act.StatusCode = 500
	res, err = c.CompareCaseOutput(comparator.ModeHTTPContract, exp, act)
	require.NoError(t, err)
	assert.False(t, res.Passed)
	assert.Contains(t, res.Mismatches[0].Path, "status_code")
}

func TestModeFilesystemSnapshot(t *testing.T) {
	c := comparator.New()

	exp := map[string]string{
		"config.json": `{"env": "prod"}`,
		"schema.sql":  `CREATE TABLE users (id INT);`,
	}

	act := map[string]string{
		"config.json": `{"env": "prod"}`,
		"schema.sql":  `CREATE TABLE users (id INT);`,
	}

	res, err := c.Compare(comparator.ModeFilesystemSnapshot, exp, act)
	require.NoError(t, err)
	assert.True(t, res.Passed)

	// Missing file in actual
	delete(act, "schema.sql")
	res, err = c.Compare(comparator.ModeFilesystemSnapshot, exp, act)
	require.NoError(t, err)
	assert.False(t, res.Passed)
	assert.Contains(t, res.Mismatches[0].Message, "missing expected file")
}

func TestBuildReportAndSaveMismatchCase(t *testing.T) {
	c := comparator.New()
	tempDir, err := os.MkdirTemp("", "ga-comparator-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	res, err := c.Compare(comparator.ModeExact, "hello", "world")
	require.NoError(t, err)
	assert.False(t, res.Passed)

	// BuildReport
	rep := c.BuildReport("auth", "case-001", comparator.ModeExact, res)
	assert.Equal(t, adapterkit.StatusFail, rep.Status)
	assert.Equal(t, "auth", rep.UnitID)
	assert.Equal(t, "case-001", rep.CaseID)
	assert.NotEmpty(t, rep.Mismatches)

	// SaveMismatchCase
	input := &adapterkit.CaseInput{Path: "/test"}
	exp := &adapterkit.CaseOutput{Stdout: "hello"}
	act := &adapterkit.CaseOutput{Stdout: "world"}

	savedPath, err := c.SaveMismatchCase(tempDir, "auth", "case-001", input, exp, act, res.Mismatches)
	require.NoError(t, err)
	assert.FileExists(t, savedPath)

	data, err := os.ReadFile(savedPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "case-001")
	assert.Contains(t, string(data), "exact byte mismatch")
}
