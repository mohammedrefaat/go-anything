// Package traps generates semantic hazard test cases for Go targets.
package traps

import (
	"fmt"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// TrapCategory defines the known semantic hazard categories when porting to Go.
type TrapCategory string

const (
	// TrapEmptyCollection covers nil slice vs empty slice serialization differences (null vs []).
	TrapEmptyCollection TrapCategory = "empty_collection"

	// TrapHTMLEscaping covers HTML character escaping in Go's default json.Marshal (<, >, &).
	TrapHTMLEscaping TrapCategory = "html_escaping"

	// TrapMapKeyOrder covers map key iteration order (Go sorts or randomizes, others preserve insertion).
	TrapMapKeyOrder TrapCategory = "map_key_order"

	// TrapNumberPrecision covers integer overflow (>2^53-1), -0.0, NaN, and float formatting.
	TrapNumberPrecision TrapCategory = "number_precision"

	// TrapStringOffset covers UTF-16 vs UTF-8 offsets, rune vs byte slicing, and \r\n vs \n.
	TrapStringOffset TrapCategory = "string_offset"

	// TrapTimeFormat covers timezone representation, ISO8601 formatting, and sub-second precision.
	TrapTimeFormat TrapCategory = "time_format"

	// TrapErrorPath covers malformed input handling, missing fields, and non-zero exit codes.
	TrapErrorPath TrapCategory = "error_path"
)

// AllTrapCategories lists all 7 Go semantic hazard categories.
var AllTrapCategories = []TrapCategory{
	TrapEmptyCollection,
	TrapHTMLEscaping,
	TrapMapKeyOrder,
	TrapNumberPrecision,
	TrapStringOffset,
	TrapTimeFormat,
	TrapErrorPath,
}

// Generator produces targeted trap cases for a migration unit.
type Generator struct {
	SourceRevision string
	Runtime        string
}

// NewGenerator initializes a trap case generator.
func NewGenerator(sourceRevision, runtime string) *Generator {
	if sourceRevision == "" {
		sourceRevision = "head"
	}
	if runtime == "" {
		runtime = "go1.22"
	}
	return &Generator{
		SourceRevision: sourceRevision,
		Runtime:        runtime,
	}
}

// GenerateCategoryTraps generates trap cases for a specific category.
func (g *Generator) GenerateCategoryTraps(unitID string, cat TrapCategory) ([]*adapterkit.Case, error) {
	switch cat {
	case TrapEmptyCollection:
		return g.generateEmptyCollection(unitID), nil
	case TrapHTMLEscaping:
		return g.generateHTMLEscaping(unitID), nil
	case TrapMapKeyOrder:
		return g.generateMapKeyOrder(unitID), nil
	case TrapNumberPrecision:
		return g.generateNumberPrecision(unitID), nil
	case TrapStringOffset:
		return g.generateStringOffset(unitID), nil
	case TrapTimeFormat:
		return g.generateTimeFormat(unitID), nil
	case TrapErrorPath:
		return g.generateErrorPath(unitID), nil
	default:
		return nil, fmt.Errorf("unknown trap category: %q", cat)
	}
}

// GenerateAllTraps generates trap cases covering all 7 semantic hazard categories.
func (g *Generator) GenerateAllTraps(unitID string) []*adapterkit.Case {
	var all []*adapterkit.Case
	for _, cat := range AllTrapCategories {
		cases, _ := g.GenerateCategoryTraps(unitID, cat)
		all = append(all, cases...)
	}
	return all
}

func (g *Generator) baseCase(unitID, caseID string, cat TrapCategory) *adapterkit.Case {
	return &adapterkit.Case{
		SchemaVersion:  adapterkit.SchemaVersionV1Alpha1,
		CaseID:         caseID,
		UnitID:         unitID,
		Kind:           adapterkit.KindTrap,
		TrapType:       string(cat),
		SourceRevision: g.SourceRevision,
		Runtime:        g.Runtime,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

// 1. Empty Collection Traps (nil slice as null vs [] in JSON)
func (g *Generator) generateEmptyCollection(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-empty-col-01", unitID), TrapEmptyCollection)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/items",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"tags": []any{},
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"tags":  []any{},
			"count": 0,
		},
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-empty-col-02", unitID), TrapEmptyCollection)
	c2.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/items",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"tags": nil,
		},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"tags":  nil,
			"count": 0,
		},
	}

	return []*adapterkit.Case{c1, c2}
}

// 2. HTML Escaping Traps (<, >, & in JSON encoding)
func (g *Generator) generateHTMLEscaping(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-html-esc-01", unitID), TrapHTMLEscaping)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/content",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"html": "<script>alert('xss')</script>",
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"html": "<script>alert('xss')</script>",
		},
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-html-esc-02", unitID), TrapHTMLEscaping)
	c2.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/entities",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"text": "A & B -> C < D",
		},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"text": "A & B -> C < D",
		},
	}

	return []*adapterkit.Case{c1, c2}
}

// 3. Map Key Order Traps (key ordering differences)
func (g *Generator) generateMapKeyOrder(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-map-order-01", unitID), TrapMapKeyOrder)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/config",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"zebra":  1,
			"apple":  2,
			"monkey": 3,
			"banana": 4,
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"zebra":  1,
			"apple":  2,
			"monkey": 3,
			"banana": 4,
		},
	}

	return []*adapterkit.Case{c1}
}

// 4. Number Precision Traps (int64 overflow >2^53-1, -0, float representation)
func (g *Generator) generateNumberPrecision(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-num-prec-01", unitID), TrapNumberPrecision)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/transactions",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"large_int": int64(9007199254740993), // > 2^53 - 1 (unsafe in JS/float64)
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"large_int": int64(9007199254740993),
		},
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-num-prec-02", unitID), TrapNumberPrecision)
	c2.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/coordinates",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"lat": 0.0,
			"lng": -0.0,
		},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"lat": 0.0,
			"lng": 0.0,
		},
	}

	return []*adapterkit.Case{c1, c2}
}

// 5. String Offset Traps (UTF-16 vs UTF-8 offsets, rune vs byte slicing, \r\n vs \n)
func (g *Generator) generateStringOffset(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-str-offset-01", unitID), TrapStringOffset)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/text/slice",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"text": "Hello 🌍 rocket 🚀 space",
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"length_runes": 23,
			"length_bytes": 29,
		},
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-str-offset-02", unitID), TrapStringOffset)
	c2.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/text/lines",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"crlf_text": "line1\r\nline2\r\nline3",
		},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"line_count": 3,
		},
	}

	return []*adapterkit.Case{c1, c2}
}

// 6. Time Format Traps (timezone offsets, format strings, sub-second precision)
func (g *Generator) generateTimeFormat(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-time-fmt-01", unitID), TrapTimeFormat)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/events",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"start_time": "2026-11-19T10:00:00.123456789Z",
		},
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"iso": "2026-11-19T10:00:00.123456789Z",
		},
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-time-fmt-02", unitID), TrapTimeFormat)
	c2.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/events",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"offset_time": "2026-11-19T12:00:00+02:00",
		},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"utc_normalized": "2026-11-19T10:00:00Z",
		},
	}

	return []*adapterkit.Case{c1, c2}
}

// 7. Error Path Traps (malformed input, missing fields, non-zero exit codes)
func (g *Generator) generateErrorPath(unitID string) []*adapterkit.Case {
	c1 := g.baseCase(unitID, fmt.Sprintf("trap-%s-err-path-01", unitID), TrapErrorPath)
	c1.Input = adapterkit.CaseInput{
		Method: "POST",
		Path:   "/users",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{}, // Missing required fields
	}
	c1.ExpectedOutput = adapterkit.CaseOutput{
		StatusCode: 400,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: map[string]any{
			"error": "missing required field 'username'",
		},
		ErrorMessage: "missing required field 'username'",
	}

	c2 := g.baseCase(unitID, fmt.Sprintf("trap-%s-err-path-02", unitID), TrapErrorPath)
	c2.Input = adapterkit.CaseInput{
		Argv: []string{"unit-cli", "--invalid-flag"},
	}
	c2.ExpectedOutput = adapterkit.CaseOutput{
		ExitCode:     2,
		Stderr:       "flag provided but not defined: -invalid-flag\n",
		ErrorMessage: "invalid CLI arguments",
	}

	return []*adapterkit.Case{c1, c2}
}
