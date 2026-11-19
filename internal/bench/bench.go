package bench

import (
	"runtime"
	"time"
)

// Metric captures latency, memory bytes, and heap allocations for a workload.
type Metric struct {
	Iterations        int           `json:"iterations"`
	TotalDuration     time.Duration `json:"total_duration"`
	DurationPerOp     time.Duration `json:"duration_per_op"`
	DurationPerOpNS   float64       `json:"duration_per_op_ns"`
	TotalAllocBytes   uint64        `json:"total_alloc_bytes"`
	AllocBytesPerOp   uint64        `json:"alloc_bytes_per_op"`
	TotalAllocObjects uint64        `json:"total_alloc_objects"`
	AllocObjectsPerOp uint64        `json:"alloc_objects_per_op"`
}

// BenchmarkResult compares performance metrics between legacy and modern implementations.
// Note: Benchmarks are informational and never block quality gates or promotions.
type BenchmarkResult struct {
	Name                string  `json:"name"`
	Legacy              Metric  `json:"legacy"`
	Modern              Metric  `json:"modern"`
	LatencyDeltaPercent float64 `json:"latency_delta_percent"` // negative means modern is faster
	AllocDeltaPercent   float64 `json:"alloc_delta_percent"`   // negative means modern allocates less
}

// Benchmark manages non-blocking performance benchmarking for ported units.
type Benchmark struct{}

// New returns a new Benchmark instance.
func New() *Benchmark {
	return &Benchmark{}
}

// Measure executes fn iterations times and records duration, heap memory allocations, and allocation count.
func (b *Benchmark) Measure(fn func(), iterations int) Metric {
	if iterations <= 0 {
		iterations = 1
	}

	// Warmup and garbage collect to stabilize measurements
	fn()
	runtime.GC()

	var mStart, mEnd runtime.MemStats
	runtime.ReadMemStats(&mStart)
	start := time.Now()

	for i := 0; i < iterations; i++ {
		fn()
	}

	duration := time.Since(start)
	runtime.ReadMemStats(&mEnd)

	allocBytes := uint64(0)
	if mEnd.TotalAlloc >= mStart.TotalAlloc {
		allocBytes = mEnd.TotalAlloc - mStart.TotalAlloc
	}

	allocObjects := uint64(0)
	if mEnd.Mallocs >= mStart.Mallocs {
		allocObjects = mEnd.Mallocs - mStart.Mallocs
	}

	durationPerOp := duration / time.Duration(iterations)
	bytesPerOp := allocBytes / uint64(iterations)
	objectsPerOp := allocObjects / uint64(iterations)

	return Metric{
		Iterations:        iterations,
		TotalDuration:     duration,
		DurationPerOp:     durationPerOp,
		DurationPerOpNS:   float64(duration.Nanoseconds()) / float64(iterations),
		TotalAllocBytes:   allocBytes,
		AllocBytesPerOp:   bytesPerOp,
		TotalAllocObjects: allocObjects,
		AllocObjectsPerOp: objectsPerOp,
	}
}

// Compare benchmarks both legacyFn and modernFn and computes relative performance deltas.
func (b *Benchmark) Compare(name string, legacyFn, modernFn func(), iterations int) *BenchmarkResult {
	legacyMetric := b.Measure(legacyFn, iterations)
	modernMetric := b.Measure(modernFn, iterations)

	var latencyDelta float64
	if legacyMetric.DurationPerOpNS > 0 {
		latencyDelta = ((modernMetric.DurationPerOpNS - legacyMetric.DurationPerOpNS) / legacyMetric.DurationPerOpNS) * 100.0
	}

	var allocDelta float64
	if legacyMetric.AllocBytesPerOp > 0 {
		allocDelta = (float64(int64(modernMetric.AllocBytesPerOp)-int64(legacyMetric.AllocBytesPerOp)) / float64(legacyMetric.AllocBytesPerOp)) * 100.0
	}

	return &BenchmarkResult{
		Name:                name,
		Legacy:              legacyMetric,
		Modern:              modernMetric,
		LatencyDeltaPercent: latencyDelta,
		AllocDeltaPercent:   allocDelta,
	}
}
