package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefaat/go-anything/adapters/source/javascript"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestJSRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	pkgJSON := `{
  "name": "sample-js-app",
  "version": "1.0.0",
  "main": "index.js",
  "scripts": {
    "start": "node index.js",
    "test": "node --test"
  },
  "dependencies": {
    "express": "^4.19.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "package.json"), []byte(pkgJSON), 0644))

	mathJS := `
function add(a, b) {
  return a + b;
}

function multiply(a, b) {
  return a * b;
}

module.exports = { add, multiply };
`
	srcDir := filepath.Join(tempDir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "math.js"), []byte(mathJS), 0644))

	indexJS := `
const { add } = require('./src/math');
console.log(add(1, 2));
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "index.js"), []byte(indexJS), 0644))

	return tempDir
}

func TestJavaScriptAdapter_ConformanceSuite(t *testing.T) {
	repoDir := createTestJSRepo(t)

	sampleCase := &adapterkit.Case{
		SchemaVersion:  adapterkit.DefaultSchemaVersion,
		CaseID:         "case-js-test-01",
		UnitID:         "src/math",
		Kind:           adapterkit.KindHappy,
		SourceRevision: "d651fa15cf1b4f2c0ce9e16d4ea244391e6005d5",
		Runtime:        "node26",
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Input: adapterkit.CaseInput{
			Method: "GET",
			Path:   "/math/add?a=1&b=2",
		},
		ExpectedOutput: adapterkit.CaseOutput{
			StatusCode: 200,
			Body:       map[string]any{"result": 3.0},
		},
	}

	sharedReplayHandler := func(ctx context.Context, c *adapterkit.Case) (*adapterkit.CaseOutput, error) {
		return &adapterkit.CaseOutput{
			StatusCode: 200,
			ExitCode:   0,
			Stdout:     `{"status":"ok"}`,
			Body:       map[string]any{"status": "ok"},
		}, nil
	}

	adapter := javascript.NewAdapter(
		javascript.WithSharedReplayHandler(sharedReplayHandler),
	)

	cfg := conformance.Config{
		RepoPath:     repoDir,
		TargetUnitID: "src/math",
		SampleCases:  []*adapterkit.Case{sampleCase},
		StrictReplay: true,
	}

	conformance.RunSuite(t, adapter, cfg)
}

func TestJavaScriptAdapter_Detect(t *testing.T) {
	ctx := context.Background()
	adapter := javascript.NewAdapter()

	t.Run("detects JS repo with package.json without tsconfig", func(t *testing.T) {
		repoDir := createTestJSRepo(t)
		detected, err := adapter.Detect(ctx, repoDir)
		require.NoError(t, err)
		assert.True(t, detected)
	})

	t.Run("detects standalone js file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "script.mjs"), []byte("console.log('hi');"), 0644))
		detected, err := adapter.Detect(ctx, dir)
		require.NoError(t, err)
		assert.True(t, detected)
	})

	t.Run("rejects empty dir", func(t *testing.T) {
		dir := t.TempDir()
		detected, err := adapter.Detect(ctx, dir)
		require.NoError(t, err)
		assert.False(t, detected)
	})
}

func TestJavaScriptAdapter_CheckEnvironment(t *testing.T) {
	ctx := context.Background()
	adapter := javascript.NewAdapter()

	err := adapter.CheckEnvironment(ctx)
	require.NoError(t, err, "Node runtime should be installed on test host")
}
