package python

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/mohammedrefaat/go-anything/pkg/adapterkit"
	"github.com/mohammedrefaat/go-anything/pkg/adapterkit/conformance"
)

// ABIConstraint defines the ABI specifications and serialization rules for Python integration.
type ABIConstraint struct {
	SupportedRuntimes   []string `json:"supported_runtimes"`
	SerializationFormat string   `json:"serialization_format"`
	TextEncoding        string   `json:"text_encoding"`
	NumericPrecision    string   `json:"numeric_precision"`
	DateTimeStandard    string   `json:"date_time_standard"`
	CSharedStatus       string   `json:"cshared_status"`
}

// MemoryOwnershipConstraint defines the memory allocation and lifetime boundaries.
type MemoryOwnershipConstraint struct {
	SharedMemoryAllowed   bool   `json:"shared_memory_allowed"`
	PointerExchange       bool   `json:"pointer_exchange"`
	PythonGCType          string `json:"python_gc_type"`
	GoGCType              string `json:"go_gc_type"`
	OwnershipModel        string `json:"ownership_model"`
	ProcessIsolationLevel string `json:"process_isolation_level"`
}

// ConcurrencyConstraint defines execution threading, async, and lock semantics.
type ConcurrencyConstraint struct {
	GILBound               bool   `json:"gil_bound"`
	SubprocessParallelism  bool   `json:"subprocess_parallelism"`
	HTTPMultiplexing       bool   `json:"http_multiplexing"`
	RecommendedConcurrency int    `json:"recommended_concurrency"`
	ThreadingModel         string `json:"threading_model"`
}

// ErrorMappingRule associates Python exceptions with HTTP status codes and exit codes.
type ErrorMappingRule struct {
	ExceptionType string `json:"exception_type"`
	HTTPStatus    int    `json:"http_status"`
	ExitCode      int    `json:"exit_code"`
	Description   string `json:"description"`
}

// BridgeLimits declares the explicit runtime boundaries between Python and Go.
type BridgeLimits struct {
	ABI             ABIConstraint              `json:"abi"`
	MemoryOwnership MemoryOwnershipConstraint  `json:"memory_ownership"`
	Concurrency     ConcurrencyConstraint      `json:"concurrency"`
	ErrorMappings   []ErrorMappingRule         `json:"error_mappings"`
	CoverageLimits  conformance.CoverageLimits `json:"coverage_limits"`
}

// DefaultBridgeLimits declares the official bridge contract for the Python adapter.
func DefaultBridgeLimits() BridgeLimits {
	return BridgeLimits{
		ABI: ABIConstraint{
			SupportedRuntimes: []string{
				"CPython 3.10",
				"CPython 3.11",
				"CPython 3.12",
				"CPython 3.13",
			},
			SerializationFormat: "JSON (RFC 8259) / Stdio Stream",
			TextEncoding:        "UTF-8",
			NumericPrecision:    "IEEE 754 double precision float / Arbitrary int",
			DateTimeStandard:    "ISO 8601 (RFC 3339)",
			CSharedStatus:       "Experimental - deferred due to GIL, fork(), and signal hazards (see ADR in cshared/)",
		},
		MemoryOwnership: MemoryOwnershipConstraint{
			SharedMemoryAllowed:   false,
			PointerExchange:       false,
			PythonGCType:          "Reference counting with cyclic GC",
			GoGCType:              "Non-generational concurrent tri-color mark-sweep",
			OwnershipModel:        "Caller-allocates / Value-copied serialization across process boundary",
			ProcessIsolationLevel: "Strict OS process isolation (Subprocess or HTTP transport)",
		},
		Concurrency: ConcurrencyConstraint{
			GILBound:               true, // Inside each Python worker
			SubprocessParallelism:  true, // Multiple subprocesses achieve true multi-core parallel dispatch
			HTTPMultiplexing:       true, // Multiple HTTP workers or ASGI/WSGI processes
			RecommendedConcurrency: 4,
			ThreadingModel:         "Multi-process master-worker architecture bypassing single-process GIL",
		},
		ErrorMappings: []ErrorMappingRule{
			{
				ExceptionType: "ValueError",
				HTTPStatus:    http.StatusBadRequest,
				ExitCode:      2,
				Description:   "Invalid value or argument provided to function",
			},
			{
				ExceptionType: "TypeError",
				HTTPStatus:    http.StatusBadRequest,
				ExitCode:      2,
				Description:   "Incompatible data type passed to function",
			},
			{
				ExceptionType: "KeyError",
				HTTPStatus:    http.StatusNotFound,
				ExitCode:      2,
				Description:   "Missing key in dictionary or lookup table",
			},
			{
				ExceptionType: "FileNotFoundError",
				HTTPStatus:    http.StatusNotFound,
				ExitCode:      2,
				Description:   "Referenced file or path does not exist",
			},
			{
				ExceptionType: "PermissionError",
				HTTPStatus:    http.StatusForbidden,
				ExitCode:      1,
				Description:   "Access denied by filesystem or OS permissions",
			},
			{
				ExceptionType: "TimeoutError",
				HTTPStatus:    http.StatusGatewayTimeout,
				ExitCode:      124,
				Description:   "Execution exceeded allotted deadline",
			},
			{
				ExceptionType: "NotImplementedError",
				HTTPStatus:    http.StatusNotImplemented,
				ExitCode:      1,
				Description:   "Feature or method not yet implemented in target",
			},
			{
				ExceptionType: "KeyboardInterrupt",
				HTTPStatus:    http.StatusInternalServerError,
				ExitCode:      130,
				Description:   "Process interrupted by SIGINT",
			},
			{
				ExceptionType: "Exception",
				HTTPStatus:    http.StatusInternalServerError,
				ExitCode:      1,
				Description:   "Generic uncaught Python exception",
			},
		},
		CoverageLimits: conformance.CoverageLimits{
			MinStatementCoverage: 0.80,
			MaxUnreplayableRatio: 0.10,
			SupportedCaseKinds: []adapterkit.CaseKind{
				adapterkit.KindHappy,
				adapterkit.KindEdge,
				adapterkit.KindTrap,
				adapterkit.KindFuzz,
				adapterkit.KindCaptured,
			},
		},
	}
}

// MapPythonError translates a Python exception name and message into HTTP and CLI exit statuses.
func MapPythonError(exceptionType, message string) (statusCode int, exitCode int, err error) {
	limits := DefaultBridgeLimits()
	for _, rule := range limits.ErrorMappings {
		if rule.ExceptionType == exceptionType {
			return rule.HTTPStatus, rule.ExitCode, fmt.Errorf("%s: %s", exceptionType, message)
		}
	}
	return http.StatusInternalServerError, 1, errors.New(message)
}
