package bridge

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/*
var templateFS embed.FS

// Generator renders sidecar bridge source files from a Manifest.
type Generator struct {
	fs embed.FS
}

// NewGenerator creates a new Generator instance using embedded templates.
func NewGenerator() *Generator {
	return &Generator{fs: templateFS}
}

// GenerateResult reports the files generated and preserved.
type GenerateResult struct {
	OutputDir      string   `json:"output_dir"`
	GeneratedFiles []string `json:"generated_files"`
	PreservedFiles []string `json:"preserved_files"`
}

// TemplateData structures the view model for template rendering.
type TemplateData struct {
	Version      string
	Unit         string
	UnitEnvVar   string
	Service      string
	ServiceTitle string
	Transport    TransportConfig
	Functions    []FunctionView
}

// FunctionView models a function for template rendering.
type FunctionView struct {
	Name           string
	CamelName      string
	LowerCamelName string
	Description    string
	Idempotent     bool
	TimeoutMS      int
	Params         []ParamView
	Returns        ReturnsConfig
	Errors         []ErrorConfig
}

// ParamView models an input parameter for template rendering.
type ParamView struct {
	Name      string
	CamelName string
	Type      string
	GoType    string
	Required  bool
}

// Generate renders sidecar files into outputDir.
// Invariant: handlers_impl.go is NEVER overwritten if it already exists on disk (Rule R-2).
func (g *Generator) Generate(m *Manifest, outputDir string) (*GenerateResult, error) {
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	data := buildTemplateData(m)

	funcMap := template.FuncMap{
		"toCamel":      toCamelCase,
		"toLowerCamel": toLowerCamelCase,
		"toGoType":     toGoType,
		"toTSType":     toTSType,
		"toEnvVar":     toEnvVar,
		"toTitle":      toCamelCase,
	}

	fileSpecs := []struct {
		tmplName         string
		outName          string
		preserveIfExists bool
		isGoSource       bool
	}{
		{"templates/sidecar_main.go.tmpl", "main.go", false, true},
		{"templates/sidecar_types.go.tmpl", "types.go", false, true},
		{"templates/sidecar_handlers.go.tmpl", "handlers.go", false, true},
		{"templates/sidecar_handlers_impl.go.tmpl", "handlers_impl.go", true, true},
		{"templates/sidecar_test.go.tmpl", "bridge_test.go", false, true},
		{"templates/wrapper_python.py.tmpl", "wrappers/bridge.py", false, false},
		{"templates/wrapper_typescript.ts.tmpl", "wrappers/bridge.ts", false, false},
		{"templates/wrapper_typescript.d.ts.tmpl", "wrappers/bridge.d.ts", false, false},
		{"templates/wrapper_python.py.tmpl", "wrapper.py", false, false},
		{"templates/wrapper_typescript.ts.tmpl", "wrapper.ts", false, false},
	}

	res := &GenerateResult{
		OutputDir: outputDir,
	}

	for _, spec := range fileSpecs {
		outPath := filepath.Join(outputDir, spec.outName)

		if spec.preserveIfExists {
			if _, err := os.Stat(outPath); err == nil {
				res.PreservedFiles = append(res.PreservedFiles, spec.outName)
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %q: %w", outPath, err)
		}

		tmplContent, err := g.fs.ReadFile(spec.tmplName)
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded template %q: %w", spec.tmplName, err)
		}

		tmpl, err := template.New(spec.tmplName).Funcs(funcMap).Parse(string(tmplContent))
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %q: %w", spec.tmplName, err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("failed to execute template %q: %w", spec.tmplName, err)
		}

		outBytes := buf.Bytes()
		if spec.isGoSource {
			formatted, err := format.Source(outBytes)
			if err == nil {
				outBytes = formatted
			}
		}

		if err := os.WriteFile(outPath, outBytes, 0644); err != nil {
			return nil, fmt.Errorf("failed to write generated file %q: %w", outPath, err)
		}

		res.GeneratedFiles = append(res.GeneratedFiles, spec.outName)
	}

	return res, nil
}

func buildTemplateData(m *Manifest) *TemplateData {
	fns := make([]FunctionView, len(m.Functions))
	for i, fn := range m.Functions {
		params := make([]ParamView, len(fn.Params))
		for j, p := range fn.Params {
			params[j] = ParamView{
				Name:      p.Name,
				CamelName: toCamelCase(p.Name),
				Type:      p.Type,
				GoType:    toGoType(p.Type),
				Required:  p.Required,
			}
		}

		fns[i] = FunctionView{
			Name:           fn.Name,
			CamelName:      toCamelCase(fn.Name),
			LowerCamelName: toLowerCamelCase(fn.Name),
			Description:    fn.Description,
			Idempotent:     fn.Idempotent,
			TimeoutMS:      fn.TimeoutMS,
			Params:         params,
			Returns:        fn.Returns,
			Errors:         fn.Errors,
		}
	}

	return &TemplateData{
		Version:      m.Version,
		Unit:         m.Unit,
		UnitEnvVar:   toEnvVar(m.Unit),
		Service:      m.Service,
		ServiceTitle: toCamelCase(m.Service),
		Transport:    m.Transport,
		Functions:    fns,
	}
}

func toCamelCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '/'
	})
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		lower := strings.ToLower(p)
		switch lower {
		case "id":
			parts[i] = "ID"
		case "url":
			parts[i] = "URL"
		case "http":
			parts[i] = "HTTP"
		case "rpc":
			parts[i] = "RPC"
		case "json":
			parts[i] = "JSON"
		case "api":
			parts[i] = "API"
		case "ms":
			parts[i] = "MS"
		default:
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toLowerCamelCase(s string) string {
	c := toCamelCase(s)
	if c == "" {
		return ""
	}
	return strings.ToLower(c[:1]) + c[1:]
}

func toGoType(t string) string {
	t = strings.TrimSpace(t)
	switch strings.ToLower(t) {
	case "string":
		return "string"
	case "int", "integer", "int32":
		return "int"
	case "int64", "long":
		return "int64"
	case "float", "float64", "double", "number":
		return "float64"
	case "bool", "boolean":
		return "bool"
	case "bytes", "binary":
		return "[]byte"
	case "object":
		return "map[string]any"
	case "[]string":
		return "[]string"
	case "[]int":
		return "[]int"
	case "[]float64":
		return "[]float64"
	case "any":
		return "any"
	default:
		return t
	}
}

func toEnvVar(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

func toTSType(t string) string {
	t = strings.TrimSpace(t)
	switch strings.ToLower(t) {
	case "string":
		return "string"
	case "int", "integer", "int32", "float", "float64", "double", "number":
		return "number"
	case "int64", "long":
		return "bigint | number"
	case "bool", "boolean":
		return "boolean"
	case "bytes", "binary":
		return "Uint8Array | string"
	case "object":
		return "Record<string, any>"
	case "[]string":
		return "string[]"
	case "[]int":
		return "number[]"
	case "[]float64":
		return "number[]"
	default:
		return "any"
	}
}
