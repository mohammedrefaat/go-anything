package parity

import (
	"math"
	"math/rand"
	"reflect"
	"strings"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
)

// FuzzConfig specifies settings for the differential fuzzer.
type FuzzConfig struct {
	UnitID     string        `json:"unit_id"`
	Iterations int           `json:"iterations"`
	Seed       int64         `json:"seed"`
	Timeout    time.Duration `json:"timeout"`
	OutputFile string        `json:"output_file,omitempty"`
}

// DefaultFuzzConfig returns a sensible baseline configuration.
func DefaultFuzzConfig(unitID string) FuzzConfig {
	return FuzzConfig{
		UnitID:     unitID,
		Iterations: 100,
		Seed:       time.Now().UnixNano(),
		Timeout:    5 * time.Second,
	}
}

// FuzzResult summarizes the output of differential fuzzing.
type FuzzResult struct {
	TotalRuns   int                `json:"total_runs"`
	Divergences []*Divergence      `json:"divergences"`
	FuzzCases   []*adapterkit.Case `json:"fuzz_cases"`
}

// DifferentialFuzzer executes mutated inputs across legacy and modern implementations.
type DifferentialFuzzer struct {
	cfg FuzzConfig
	rnd *rand.Rand
}

// NewDifferentialFuzzer initializes a new fuzzer with the given configuration.
func NewDifferentialFuzzer(cfg FuzzConfig) *DifferentialFuzzer {
	if cfg.Iterations <= 0 {
		cfg.Iterations = 50
	}
	if cfg.Seed == 0 {
		cfg.Seed = 1337
	}
	return &DifferentialFuzzer{
		cfg: cfg,
		rnd: rand.New(rand.NewSource(cfg.Seed)),
	}
}

// Fuzz runs differential fuzzing on legacyFn and modernFn, logging any divergence as a KindFuzz case.
func (df *DifferentialFuzzer) Fuzz(fnName string, legacyFn, modernFn any, seedInputs [][]any) (*FuzzResult, error) {
	result := &FuzzResult{}
	caseIndex := 0

	// 1. Run baseline seeds
	for _, seeds := range seedInputs {
		result.TotalRuns++
		legArgs := DeepCopyArgs(seeds)
		modArgs := DeepCopyArgs(seeds)

		legRes := InvokeSafe(legacyFn, legArgs...)
		modRes := InvokeSafe(modernFn, modArgs...)

		if div := CompareInvocations(fnName, seeds, legRes, modRes); div != nil {
			div.Kind = adapterkit.KindFuzz
			caseIndex++
			fuzzCase := div.ToCase(df.cfg.UnitID, caseIndex)
			fuzzCase.Kind = adapterkit.KindFuzz
			result.Divergences = append(result.Divergences, div)
			result.FuzzCases = append(result.FuzzCases, fuzzCase)

			if df.cfg.OutputFile != "" {
				_ = AppendCase(df.cfg.OutputFile, fuzzCase)
			}
		}
	}

	// 2. Generate mutated variations
	if len(seedInputs) == 0 {
		return result, nil
	}

	for i := 0; i < df.cfg.Iterations; i++ {
		result.TotalRuns++
		baseSeed := seedInputs[df.rnd.Intn(len(seedInputs))]
		mutated := df.mutateArgs(baseSeed)

		legArgs := DeepCopyArgs(mutated)
		modArgs := DeepCopyArgs(mutated)

		legRes := InvokeSafe(legacyFn, legArgs...)
		modRes := InvokeSafe(modernFn, modArgs...)

		if div := CompareInvocations(fnName, mutated, legRes, modRes); div != nil {
			div.Kind = adapterkit.KindFuzz
			caseIndex++
			fuzzCase := div.ToCase(df.cfg.UnitID, caseIndex)
			fuzzCase.Kind = adapterkit.KindFuzz
			result.Divergences = append(result.Divergences, div)
			result.FuzzCases = append(result.FuzzCases, fuzzCase)

			if df.cfg.OutputFile != "" {
				_ = AppendCase(df.cfg.OutputFile, fuzzCase)
			}
		}
	}

	return result, nil
}

func (df *DifferentialFuzzer) mutateArgs(args []any) []any {
	if len(args) == 0 {
		return args
	}

	out := make([]any, len(args))
	for i, a := range args {
		out[i] = df.mutateValue(DeepCopy(a))
	}
	return out
}

func (df *DifferentialFuzzer) mutateValue(val any) any {
	if val == nil {
		return nil
	}

	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i := v.Int()
		strategy := df.rnd.Intn(6)
		switch strategy {
		case 0:
			return reflect.ValueOf(int64(0)).Convert(v.Type()).Interface()
		case 1:
			return reflect.ValueOf(int64(-1)).Convert(v.Type()).Interface()
		case 2:
			return reflect.ValueOf(int64(1)).Convert(v.Type()).Interface()
		case 3:
			return reflect.ValueOf(int64(math.MaxInt32)).Convert(v.Type()).Interface()
		case 4:
			return reflect.ValueOf(int64(math.MinInt32)).Convert(v.Type()).Interface()
		default:
			delta := int64(df.rnd.Intn(21) - 10)
			return reflect.ValueOf(i + delta).Convert(v.Type()).Interface()
		}

	case reflect.Float32, reflect.Float64:
		strategy := df.rnd.Intn(5)
		switch strategy {
		case 0:
			return reflect.ValueOf(0.0).Convert(v.Type()).Interface()
		case 1:
			return reflect.ValueOf(-0.0).Convert(v.Type()).Interface()
		case 2:
			return reflect.ValueOf(math.NaN()).Convert(v.Type()).Interface()
		case 3:
			return reflect.ValueOf(math.Inf(1)).Convert(v.Type()).Interface()
		default:
			f := v.Float() + (df.rnd.Float64() - 0.5)
			return reflect.ValueOf(f).Convert(v.Type()).Interface()
		}

	case reflect.String:
		s := v.String()
		strategy := df.rnd.Intn(6)
		switch strategy {
		case 0:
			return ""
		case 1:
			return "\x00\x00\x00" // null bytes
		case 2:
			return "%s%d%v%x" // format string traps
		case 3:
			return "日本語🚀🔥\r\n\t" // unicode and newline boundaries
		case 4:
			return s + strings.Repeat("A", 256) // buffer expansion
		default:
			if len(s) > 0 {
				idx := df.rnd.Intn(len(s))
				return s[:idx] + string(rune('a'+df.rnd.Intn(26))) + s[idx+1:]
			}
			return "fuzzed"
		}

	case reflect.Slice:
		if v.Len() == 0 {
			return val
		}
		strategy := df.rnd.Intn(3)
		switch strategy {
		case 0:
			return reflect.MakeSlice(v.Type(), 0, 0).Interface() // empty slice
		case 1:
			// Truncate
			newLen := df.rnd.Intn(v.Len())
			return v.Slice(0, newLen).Interface()
		default:
			// Mutate one element
			idx := df.rnd.Intn(v.Len())
			elem := v.Index(idx).Interface()
			mutElem := df.mutateValue(elem)
			newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			reflect.Copy(newSlice, v)
			newSlice.Index(idx).Set(reflect.ValueOf(mutElem).Convert(v.Type().Elem()))
			return newSlice.Interface()
		}

	case reflect.Bool:
		return !v.Bool()

	default:
		return val
	}
}
