package bridge_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammedrefaat/go-anything/internal/bridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sampleYAML = `
version: "v1alpha1"
unit: "pkg/billing"
service: "billing-bridge"
transport:
  default: "stdio"
  socket_path: "/tmp/go-anything-billing.sock"
  pipe_name: "\\\\.\\pipe\\go-anything-billing"
functions:
  - name: "calculate_tax"
    description: "Computes sales tax based on currency and customer jurisdiction."
    idempotent: true
    timeout_ms: 1500
    params:
      - name: "amount"
        type: "float64"
        required: true
      - name: "currency"
        type: "string"
        required: true
      - name: "customer_id"
        type: "string"
        required: false
    returns:
      type: "object"
      properties:
        tax_amount: "float64"
        rate_applied: "float64"
        exemption: "boolean"
    errors:
      - code: -32001
        name: "InvalidCurrencyError"
        description: "Provided currency code is not supported."
`

func TestManifest_LoadAndSave(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "bridge.yaml")

	err := os.WriteFile(manifestPath, []byte(sampleYAML), 0644)
	require.NoError(t, err)

	m, err := bridge.LoadManifest(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, "v1alpha1", m.Version)
	assert.Equal(t, "pkg/billing", m.Unit)
	assert.Equal(t, "billing-bridge", m.Service)
	assert.Equal(t, "stdio", m.Transport.Default)
	require.Len(t, m.Functions, 1)

	fn := m.Functions[0]
	assert.Equal(t, "calculate_tax", fn.Name)
	assert.True(t, fn.Idempotent)
	assert.Equal(t, 1500, fn.TimeoutMS)
	require.Len(t, fn.Params, 3)
	assert.Equal(t, "amount", fn.Params[0].Name)
	assert.Equal(t, "float64", fn.Params[0].Type)
	assert.True(t, fn.Params[0].Required)

	assert.Equal(t, "object", fn.Returns.Type)
	assert.Equal(t, "float64", fn.Returns.Properties["tax_amount"])

	require.Len(t, fn.Errors, 1)
	assert.Equal(t, -32001, fn.Errors[0].Code)
	assert.Equal(t, "InvalidCurrencyError", fn.Errors[0].Name)

	// Round-trip save and reload
	savePath := filepath.Join(tempDir, "bridge_saved.yaml")
	err = bridge.SaveManifest(savePath, m)
	require.NoError(t, err)

	reloaded, err := bridge.LoadManifest(savePath)
	require.NoError(t, err)
	assert.Equal(t, m.Unit, reloaded.Unit)
	assert.Equal(t, m.Service, reloaded.Service)
	assert.Equal(t, len(m.Functions), len(reloaded.Functions))
}

func TestManifest_ValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		manifest bridge.Manifest
		errMsg   string
	}{
		{
			name:     "missing version",
			manifest: bridge.Manifest{Unit: "pkg/foo", Functions: []bridge.Function{{Name: "bar", Returns: bridge.ReturnsConfig{Type: "string"}}}},
			errMsg:   "version is required",
		},
		{
			name:     "missing unit",
			manifest: bridge.Manifest{Version: "v1", Functions: []bridge.Function{{Name: "bar", Returns: bridge.ReturnsConfig{Type: "string"}}}},
			errMsg:   "unit is required",
		},
		{
			name:     "no functions",
			manifest: bridge.Manifest{Version: "v1", Unit: "pkg/foo"},
			errMsg:   "at least one function",
		},
		{
			name: "unnamed function",
			manifest: bridge.Manifest{
				Version:   "v1",
				Unit:      "pkg/foo",
				Functions: []bridge.Function{{Returns: bridge.ReturnsConfig{Type: "string"}}},
			},
			errMsg: "must have a name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.manifest.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestGenerator_GenerateAllFiles(t *testing.T) {
	tempDir := t.TempDir()
	outDir := filepath.Join(tempDir, "sidecar")

	gen := bridge.NewGenerator()

	manifest := &bridge.Manifest{
		Version: "v1alpha1",
		Unit:    "pkg/billing",
		Service: "billing-bridge",
		Functions: []bridge.Function{
			{
				Name:        "calculate_tax",
				Description: "Computes sales tax.",
				Idempotent:  true,
				TimeoutMS:   1000,
				Params: []bridge.Param{
					{Name: "amount", Type: "float64", Required: true},
					{Name: "currency", Type: "string", Required: true},
				},
				Returns: bridge.ReturnsConfig{
					Type: "object",
					Properties: map[string]string{
						"tax_amount": "float64",
					},
				},
				Errors: []bridge.ErrorConfig{
					{Code: -32001, Name: "InvalidCurrencyError"},
				},
			},
		},
	}

	res, err := gen.Generate(manifest, outDir)
	require.NoError(t, err)

	expectedFiles := []string{
		"main.go",
		"types.go",
		"handlers.go",
		"handlers_impl.go",
		"bridge_test.go",
		"wrapper.py",
		"wrapper.ts",
	}

	for _, f := range expectedFiles {
		assert.Contains(t, res.GeneratedFiles, f)
		fullPath := filepath.Join(outDir, f)
		assert.FileExists(t, fullPath)

		content, err := os.ReadFile(fullPath)
		require.NoError(t, err)

		if strings.HasSuffix(f, ".go") && f != "handlers_impl.go" {
			assert.Contains(t, string(content), "// Code generated by go-anything bridge; DO NOT EDIT.")
		}
		if f == "handlers_impl.go" {
			assert.Contains(t, string(content), "// TODO(port)")
			assert.Contains(t, string(content), "ErrNotPorted")
		}
		if strings.HasSuffix(f, ".py") {
			assert.Contains(t, string(content), "# Code generated by go-anything bridge; DO NOT EDIT.")
			assert.Contains(t, string(content), "BillingBridgeClient")
		}
		if strings.HasSuffix(f, ".ts") {
			assert.Contains(t, string(content), "// Code generated by go-anything bridge; DO NOT EDIT.")
			assert.Contains(t, string(content), "BillingBridgeClient")
		}
	}
}

func TestGenerator_IdempotentRegenerationPreservesHandlersImpl(t *testing.T) {
	tempDir := t.TempDir()
	outDir := filepath.Join(tempDir, "sidecar")

	gen := bridge.NewGenerator()

	manifest := &bridge.Manifest{
		Version: "v1alpha1",
		Unit:    "pkg/billing",
		Service: "billing-bridge",
		Functions: []bridge.Function{
			{
				Name: "calculate_tax",
				Params: []bridge.Param{
					{Name: "amount", Type: "float64", Required: true},
				},
				Returns: bridge.ReturnsConfig{Type: "float64"},
			},
		},
	}

	// 1. Initial Generation
	res1, err := gen.Generate(manifest, outDir)
	require.NoError(t, err)
	assert.Contains(t, res1.GeneratedFiles, "handlers_impl.go")
	assert.Empty(t, res1.PreservedFiles)

	implFile := filepath.Join(outDir, "handlers_impl.go")
	initialContent, err := os.ReadFile(implFile)
	require.NoError(t, err)
	assert.Contains(t, string(initialContent), "ErrNotPorted")

	// 2. User implements custom business logic (Rule R-2)
	customLogic := string(initialContent) + "\n// CUSTOM USER LOGIC: tax rate is 0.15\n"
	err = os.WriteFile(implFile, []byte(customLogic), 0644)
	require.NoError(t, err)

	// 3. Update manifest (e.g. add a second function) and regenerate
	manifest.Functions = append(manifest.Functions, bridge.Function{
		Name:    "refund_tax",
		Returns: bridge.ReturnsConfig{Type: "bool"},
	})

	res2, err := gen.Generate(manifest, outDir)
	require.NoError(t, err)

	// handlers_impl.go MUST be preserved and not overwritten!
	assert.Contains(t, res2.PreservedFiles, "handlers_impl.go")
	assert.NotContains(t, res2.GeneratedFiles, "handlers_impl.go")

	currentContent, err := os.ReadFile(implFile)
	require.NoError(t, err)
	assert.Contains(t, string(currentContent), "// CUSTOM USER LOGIC: tax rate is 0.15")

	// Other generated files (like handlers.go) MUST reflect the updated manifest
	handlersContent, err := os.ReadFile(filepath.Join(outDir, "handlers.go"))
	require.NoError(t, err)
	assert.Contains(t, string(handlersContent), "RefundTax")
}

func TestGenerator_GeneratedCodeCompiles(t *testing.T) {
	tempDir := t.TempDir()
	outDir := filepath.Join(tempDir, "sidecar")

	gen := bridge.NewGenerator()

	manifest := &bridge.Manifest{
		Version: "v1alpha1",
		Unit:    "pkg/billing",
		Service: "billing-bridge",
		Functions: []bridge.Function{
			{
				Name: "calculate_tax",
				Params: []bridge.Param{
					{Name: "amount", Type: "float64", Required: true},
					{Name: "currency", Type: "string", Required: true},
				},
				Returns: bridge.ReturnsConfig{
					Type: "object",
					Properties: map[string]string{
						"tax_amount": "float64",
					},
				},
			},
		},
	}

	_, err := gen.Generate(manifest, outDir)
	require.NoError(t, err)

	// Build the sidecar binary to confirm all generated source files compile cleanly into an executable
	binName := "sidecar.exe"
	binPath := filepath.Join(outDir, binName)
	cmd := exec.Command("go", "build", "-o", binPath, "main.go", "types.go", "handlers.go", "handlers_impl.go")
	cmd.Dir = outDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Generated code build output: %s", string(output))
	}
	require.NoError(t, err, "generated Go sidecar must compile into binary: %s", string(output))
	assert.FileExists(t, binPath)
}

func TestGenerator_BridgeTestPasses(t *testing.T) {
	testDir := filepath.Join("testdata", "compiled_sidecar")
	_ = os.RemoveAll(testDir)
	defer os.RemoveAll(testDir)

	gen := bridge.NewGenerator()
	manifest := &bridge.Manifest{
		Version: "v1alpha1",
		Unit:    "pkg/billing",
		Service: "billing-bridge",
		Functions: []bridge.Function{
			{
				Name: "calculate_tax",
				Params: []bridge.Param{
					{Name: "amount", Type: "float64", Required: true},
				},
				Returns: bridge.ReturnsConfig{Type: "float64"},
			},
		},
	}

	_, err := gen.Generate(manifest, testDir)
	require.NoError(t, err)

	cmd := exec.Command("go", "test", "-v", "./internal/bridge/testdata/compiled_sidecar")
	cmd.Dir = "../.."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Bridge test output: %s", string(output))
	}
	require.NoError(t, err, "generated bridge test must pass: %s", string(output))
}

func TestGenerator_WrappersGenerationAndContent(t *testing.T) {
	tempDir := t.TempDir()
	outDir := filepath.Join(tempDir, "sidecar")

	gen := bridge.NewGenerator()
	manifest := &bridge.Manifest{
		Version: "v1alpha1",
		Unit:    "pkg/billing",
		Service: "billing-bridge",
		Functions: []bridge.Function{
			{
				Name:        "calculate_tax",
				Description: "Calculates sales tax.",
				Params: []bridge.Param{
					{Name: "amount", Type: "float64", Required: true},
					{Name: "currency", Type: "string", Required: true},
				},
				Returns: bridge.ReturnsConfig{
					Type: "object",
					Properties: map[string]string{
						"tax_amount": "float64",
					},
				},
			},
		},
	}

	res, err := gen.Generate(manifest, outDir)
	require.NoError(t, err)

	assert.Contains(t, res.GeneratedFiles, "wrappers/bridge.py")
	assert.Contains(t, res.GeneratedFiles, "wrappers/bridge.ts")
	assert.Contains(t, res.GeneratedFiles, "wrappers/bridge.d.ts")

	// 1. Python wrapper checks
	pyPath := filepath.Join(outDir, "wrappers", "bridge.py")
	assert.FileExists(t, pyPath)
	pyContent, err := os.ReadFile(pyPath)
	require.NoError(t, err)
	pyStr := string(pyContent)

	assert.Contains(t, pyStr, "# Code generated by go-anything bridge; DO NOT EDIT.")
	assert.Contains(t, pyStr, "class BillingBridgeClient:")
	assert.Contains(t, pyStr, "threading.Lock()")
	assert.Contains(t, pyStr, "atexit.register(self.close)")
	assert.Contains(t, pyStr, "Content-Length:")
	assert.Contains(t, pyStr, "def _original_calculate_tax")
	assert.Contains(t, pyStr, `GO_ANYTHING_BRIDGE`)
	assert.Contains(t, pyStr, `GO_ANYTHING_UNIT_pkg_billing`)
	assert.Contains(t, pyStr, "def calculate_tax(self, amount: Any = None, currency: Any = None, **kwargs) -> Any:")
	assert.Contains(t, pyStr, "def get_client() -> BillingBridgeClient:")

	// 2. TypeScript wrapper checks
	tsPath := filepath.Join(outDir, "wrappers", "bridge.ts")
	assert.FileExists(t, tsPath)
	tsContent, err := os.ReadFile(tsPath)
	require.NoError(t, err)
	tsStr := string(tsContent)

	assert.Contains(t, tsStr, "// Code generated by go-anything bridge; DO NOT EDIT.")
	assert.Contains(t, tsStr, "export class BillingBridgeClient {")
	assert.Contains(t, tsStr, "spawn(this.binaryPath")
	assert.Contains(t, tsStr, "pending = new Map")
	assert.Contains(t, tsStr, "content-length:")
	assert.Contains(t, tsStr, "export function _original_calculate_tax")
	assert.Contains(t, tsStr, "process.env.GO_ANYTHING_BRIDGE")
	assert.Contains(t, tsStr, `process.env["GO_ANYTHING_UNIT_pkg_billing"]`)
	assert.Contains(t, tsStr, "public async calculateTax(params: any = {}): Promise<any> {")

	// 3. TypeScript declaration checks
	dtsPath := filepath.Join(outDir, "wrappers", "bridge.d.ts")
	assert.FileExists(t, dtsPath)
	dtsContent, err := os.ReadFile(dtsPath)
	require.NoError(t, err)
	dtsStr := string(dtsContent)

	assert.Contains(t, dtsStr, "// Code generated by go-anything bridge; DO NOT EDIT.")
	assert.Contains(t, dtsStr, "export interface CalculateTaxParams {")
	assert.Contains(t, dtsStr, "amount: number;")
	assert.Contains(t, dtsStr, "currency: string;")
	assert.Contains(t, dtsStr, "export interface CalculateTaxResult {")
	assert.Contains(t, dtsStr, "tax_amount: number;")
	assert.Contains(t, dtsStr, "export function _original_calculate_tax")
	assert.Contains(t, dtsStr, "calculateTax(params?: CalculateTaxParams): Promise<CalculateTaxResult>;")
}
