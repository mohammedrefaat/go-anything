package bench

// Input defines the parameters for the benchmark use-case.
type Input struct {
	UnitID     string `json:"unit_id"`
	Iterations int    `json:"iterations"` // default: 100
	Warmup     int    `json:"warmup"`     // default: 10
	Target     string `json:"target,omitempty"`
	Dir        string `json:"dir"`
}

// LatencyPercentiles holds p50, p95, p99, throughput, and mean latency in nanoseconds.
type LatencyPercentiles struct {
	P50NS            float64 `json:"p50_ns"`
	P95NS            float64 `json:"p95_ns"`
	P99NS            float64 `json:"p99_ns"`
	ThroughputOpsSec float64 `json:"throughput_ops_sec"`
	MeanNS           float64 `json:"mean_ns"`
}

// GoBenchmarkMetrics holds latency and memory allocation metrics for the Go implementation.
type GoBenchmarkMetrics struct {
	P50NS            float64 `json:"p50_ns"`
	P95NS            float64 `json:"p95_ns"`
	P99NS            float64 `json:"p99_ns"`
	ThroughputOpsSec float64 `json:"throughput_ops_sec"`
	MeanNS           float64 `json:"mean_ns"`
	AllocsPerOp      uint64  `json:"allocs_per_op"`
	BytesPerOp       uint64  `json:"bytes_per_op"`
}

// EnvironmentInfo captures hardware and runtime specifications for reproducibility.
type EnvironmentInfo struct {
	CPUModel        string `json:"cpu_model"`
	OS              string `json:"os"`
	GoVersion       string `json:"go_version"`
	OriginalRuntime string `json:"original_runtime"`
	Cores           int    `json:"cores"`
}

// Output encapsulates the comparative benchmark report.
type Output struct {
	SchemaVersion string             `json:"schema_version,omitempty"`
	UnitID        string             `json:"unit_id"`
	Original      LatencyPercentiles `json:"original"`
	Go            GoBenchmarkMetrics `json:"go"`
	SpeedupRatio  float64            `json:"speedup_ratio"`
	Iterations    int                `json:"iterations"`
	Warmup        int                `json:"warmup"`
	Environment   EnvironmentInfo    `json:"environment"`
	Summary       string             `json:"summary"`
	Timestamp     string             `json:"timestamp"`
}
