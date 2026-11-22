package shadow

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
)

// StartupWarning is printed at startup when non-idempotent shadowing is enabled.
// Matches the exact text required by the v3 plan.
const StartupWarning = "Shadow execution runs the Go handler on real requests. Non-idempotent handlers may repeat writes or external calls. Run against isolated data or stubs. You are responsible for the consequences."

// DefaultReportsPath is the default path for shadow mismatch logs.
const DefaultReportsPath = ".go-anything/reports/shadow_mismatches.jsonl"

// Policy configures shadow traffic mirroring rules and safety guards.
type Policy struct {
	// AllowSideEffects gates shadowing of non-idempotent requests (POST, PUT, DELETE, etc.).
	// Default is false (idempotent-only safety: GET, HEAD).
	AllowSideEffects bool

	// SampleRate defines the proportion of eligible requests shadowed (0.0 to 1.0).
	// Default is 1.0 (100% of eligible requests).
	SampleRate float64

	// Workers defines the number of concurrent worker goroutines.
	Workers int

	// QueueCap defines the maximum number of queued shadow tasks before dropping.
	QueueCap int

	// ReportsPath defines the output JSONL file path for redacted mismatch reports.
	ReportsPath string

	// WarningWriter specifies the destination for startup warnings. Defaults to os.Stderr.
	WarningWriter io.Writer

	// UnitID specifies the migration unit identifier.
	UnitID string

	// Mode specifies the default routing mode.
	Mode Mode

	// Rng is an optional custom pseudo-random number generator for deterministic sampling.
	Rng *rand.Rand
}

// DefaultPolicy returns safe, production-ready defaults.
func DefaultPolicy() Policy {
	return Policy{
		AllowSideEffects: false,
		SampleRate:       1.0,
		Workers:          4,
		QueueCap:         100,
		ReportsPath:      DefaultReportsPath,
		WarningWriter:    os.Stderr,
		Mode:             ModeShadow,
	}
}

// IsIdempotentMethod checks if the HTTP method is considered idempotent (GET, HEAD).
// Per architecture specification, only GET and HEAD requests are shadowed by default.
func IsIdempotentMethod(method string) bool {
	m := strings.ToUpper(strings.TrimSpace(method))
	return m == "GET" || m == "HEAD"
}

// EmitStartupWarning writes the mandatory side-effects warning to the configured writer.
func (p *Policy) EmitStartupWarning() {
	if p.AllowSideEffects {
		w := p.WarningWriter
		if w == nil {
			w = os.Stderr
		}
		fmt.Fprintln(w, StartupWarning)
	}
}
